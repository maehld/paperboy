package main

// The login helper runs the OAuth2 authorization code flow with PKCE once, by
// hand: it prints the URL, takes the code back from the terminal and prints the
// refresh token and the IDs of the account. Paperboy never sees the password.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// login runs the interactive flow and prints the configuration.
func (v *vicare) login(client string) error {
	reader := bufio.NewReader(os.Stdin)
	if client == "" {
		client = os.Getenv("VICARE_CLIENT_ID")
	}
	if strings.TrimSpace(client) == "" {
		fmt.Fprint(os.Stderr, "client ID from the developer portal: ")
		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return errors.New("no client ID")
		}
		client = line
	}
	v.clientID = strings.TrimSpace(client)
	if v.clientID == "" {
		return errors.New("no client ID")
	}

	verifier, challenge, err := pkce()
	if err != nil {
		return err
	}
	state, err := randomState()
	if err != nil {
		return err
	}

	fmt.Println("Open this address in a browser and log in with your ViCare account:")
	fmt.Println()
	fmt.Println("  " + v.authorizeURL(challenge, state))
	fmt.Println()
	fmt.Println("The browser cannot open the vicare:// address; its address bar shows the")
	fmt.Println("code. Paste the whole address or only the code.")
	fmt.Print("code: ")
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return errors.New("no code")
	}
	code, err := codeFrom(line)
	if err != nil {
		return err
	}

	ctx := context.Background()
	tok, err := v.exchangeCode(ctx, code, verifier)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.tokens = tok
	v.mu.Unlock()

	list, err := v.installations(ctx)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("Refresh token, put it into VICARE_REFRESH_TOKEN (or another secret store):")
	fmt.Println()
	fmt.Println("  " + tok.Refresh)
	fmt.Println()

	var firstInstallation, firstGateway, heatingDevice string
	devices := 0
	for _, i := range list {
		if firstInstallation == "" {
			firstInstallation = i.ID
		}
		fmt.Printf("installation %s\n", i.ID)
		for _, g := range i.Gateways {
			if firstGateway == "" {
				firstGateway = g.Serial
			}
			fmt.Printf("  gateway %s\n", g.Serial)
			for _, d := range g.Devices {
				devices++
				fmt.Printf("    device %s  %s  %s  %s\n", d.ID, d.ModelID, d.DeviceType, d.Status)
				if heatingDevice == "" && d.DeviceType == "heating" {
					heatingDevice = d.ID
				}
			}
		}
	}
	if devices == 0 {
		return errors.New("the account has no device: connect the heating system in the ViCare app first")
	}
	if heatingDevice == "" {
		fmt.Println()
		fmt.Println("No device of type heating: set device by hand.")
	}

	fmt.Println()
	fmt.Println("plugins:")
	fmt.Println("  vicare:")
	fmt.Println("    config:")
	fmt.Printf("      client_id: %q\n", v.clientID)
	fmt.Println("      refresh_token: ${VICARE_REFRESH_TOKEN}")
	if len(list) > 1 {
		fmt.Println("      # the account has several installations: set installation, gateway and device")
	}
	fmt.Printf("      installation: %q\n", firstInstallation)
	fmt.Printf("      gateway: %q\n", firstGateway)
	if heatingDevice != "" {
		fmt.Printf("      device: %q\n", heatingDevice)
	}
	return nil
}

// randomState returns the OAuth2 state of the flow.
func randomState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// codeFrom reads the code from a pasted redirect address or from a bare code.
func codeFrom(line string) (string, error) {
	line = strings.TrimSpace(strings.Trim(line, `"'`))
	if line == "" {
		return "", errors.New("no code")
	}
	if u, err := url.Parse(line); err == nil && (u.Scheme != "" || strings.Contains(line, "code=")) {
		q := u.Query()
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			if desc == "" {
				desc = e
			}
			return "", fmt.Errorf("the login was refused: %s", desc)
		}
		if code := q.Get("code"); code != "" {
			return code, nil
		}
		return "", errors.New("the address carries no code")
	}
	return line, nil
}
