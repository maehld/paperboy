package main

// This file speaks to the Viessmann cloud: the OAuth2 token endpoint, the
// installations of the account and the features of one heating device.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultBase is the API of the Viessmann Climate Solutions platform; the
	// old viessmann.com domain stopped in 2025.
	defaultBase = "https://api.viessmann-climatesolutions.com"
	// defaultIAM issues the tokens.
	defaultIAM = "https://iam.viessmann-climatesolutions.com"
	// redirectURI is registered for the personal client in the developer
	// portal. The app shows it after the login, so the helper can read the
	// code from it.
	redirectURI = "vicare://oauth-callback/everest"
	// scopes: IoT reads the installation, offline_access returns a refresh
	// token.
	scopes = "IoT User offline_access"
	// requestTimeout bounds one HTTP call.
	requestTimeout = 30 * time.Second
	// tokenSlack renews the access token before it expires.
	tokenSlack = 2 * time.Minute
	// maxBody caps one JSON answer.
	maxBody = 8 << 20
)

// tokens is what the plugin keeps between restarts; the refresh token rotates,
// so the stored one wins over the one in the configuration.
type tokens struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh"`
	Expiry  time.Time `json:"expiry"`
}

// tokenAnswer is the answer of the token endpoint.
type tokenAnswer struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// pkce returns a code verifier and its S256 challenge (RFC 7636).
func pkce() (verifier, challenge string, err error) {
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// authorizeURL is the page the user opens to log in to their ViCare account.
func (v *vicare) authorizeURL(challenge, state string) string {
	q := url.Values{
		"client_id":             {v.clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {scopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return v.iam + "/idp/v3/authorize?" + q.Encode()
}

// tokenRequest posts to the token endpoint and returns the parsed answer.
func (v *vicare) tokenRequest(ctx context.Context, form url.Values) (*tokenAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.iam+"/idp/v3/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	var answer tokenAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("token endpoint: HTTP %d", resp.StatusCode)
	}
	if answer.Error != "" {
		detail := answer.ErrorDescription
		if detail == "" {
			detail = answer.Error
		}
		return nil, fmt.Errorf("token endpoint: %s", detail)
	}
	if resp.StatusCode != http.StatusOK || answer.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint: HTTP %d", resp.StatusCode)
	}
	return &answer, nil
}

// exchangeCode turns the code of the login into tokens. The helper calls it
// once; the plugin itself only refreshes.
func (v *vicare) exchangeCode(ctx context.Context, code, verifier string) (*tokens, error) {
	answer, err := v.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {v.clientID},
		"code_verifier": {verifier},
	})
	if err != nil {
		return nil, err
	}
	expiry := time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	return &tokens{Access: answer.AccessToken, Refresh: answer.RefreshToken, Expiry: expiry}, nil
}

// renew exchanges the refresh token for a new access token. Viessmann may
// rotate the refresh token; the answer's one replaces the stored one.
func (v *vicare) renew(ctx context.Context, refresh string) (*tokens, error) {
	answer, err := v.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {v.clientID},
	})
	if err != nil {
		return nil, err
	}
	if answer.RefreshToken == "" {
		answer.RefreshToken = refresh
	}
	expiry := time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	return &tokens{Access: answer.AccessToken, Refresh: answer.RefreshToken, Expiry: expiry}, nil
}

// access returns a usable access token, renewing it when it is about to
// expire. A renewed refresh token goes back to Paperboy with the next answer.
func (v *vicare) access(ctx context.Context) (string, error) {
	v.mu.Lock()
	t := v.tokens
	v.mu.Unlock()
	if t != nil && t.Access != "" && time.Now().Add(tokenSlack).Before(t.Expiry) {
		return t.Access, nil
	}
	refresh := v.seedRefresh
	if t != nil && t.Refresh != "" {
		refresh = t.Refresh
	}
	if refresh == "" {
		return "", errors.New("no refresh token: run the plugin with -login")
	}
	next, err := v.renew(ctx, refresh)
	if err != nil {
		return "", fmt.Errorf("renew the access token: %w", err)
	}
	v.mu.Lock()
	v.tokens = next
	v.queueLocked()
	v.mu.Unlock()
	return next.Access, nil
}

// invalidate drops the access token, so the next call renews it.
func (v *vicare) invalidate() {
	v.mu.Lock()
	if v.tokens != nil {
		v.tokens = &tokens{Refresh: v.tokens.Refresh}
	}
	v.mu.Unlock()
}

// apiGet reads one API path with the current access token. A 401 renews the
// token once and repeats the call.
func (v *vicare) apiGet(ctx context.Context, path string) ([]byte, int, http.Header, error) {
	for attempt := 0; attempt < 2; attempt++ {
		access, err := v.access(ctx)
		if err != nil {
			return nil, 0, nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.base+path, nil)
		if err != nil {
			cancel()
			return nil, 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "paperboy-vicare/"+version())

		resp, err := v.client.Do(req)
		if err != nil {
			cancel()
			return nil, 0, nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		cancel()
		if err != nil {
			return nil, 0, nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			v.invalidate()
			continue
		}
		if expiredInBody(body) && attempt == 0 {
			v.invalidate()
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, 0, nil, errors.New("the API refused the token: run the plugin with -login again")
		}
		return body, resp.StatusCode, resp.Header, nil
	}
	return nil, 0, nil, errors.New("the API refused the token")
}

// expiredInBody detects the API's own expiry answer, which arrives with HTTP
// 200: {"error":"EXPIRED TOKEN"}.
func expiredInBody(body []byte) bool {
	var e struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &e) == nil && strings.EqualFold(e.Error, "EXPIRED TOKEN")
}

// apiError is an error answer of the API.
type apiError struct {
	status int
	kind   string
	detail string
}

func (e *apiError) Error() string {
	switch e.kind {
	case "PACKAGE_NOT_PAID_FOR":
		return "the Viessmann API package is not booked for this account"
	case "DEVICE_COMMUNICATION_ERROR":
		return "the gateway cannot reach the heating system right now"
	case "RATE_LIMIT":
		return "rate limit exceeded: the ViCare API allows about 1450 calls a day"
	}
	msg := fmt.Sprintf("HTTP %d", e.status)
	if e.detail != "" {
		msg += ": " + e.detail
	}
	return msg
}

// problem turns a non-200 answer into an apiError.
func problem(status int, body []byte) error {
	var p struct {
		StatusCode      int    `json:"statusCode"`
		ErrorType       string `json:"errorType"`
		Message         string `json:"message"`
		ExtendedPayload struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		} `json:"extendedPayload"`
	}
	_ = json.Unmarshal(body, &p)
	e := &apiError{status: status, kind: p.ErrorType, detail: p.Message}
	if e.kind == "" {
		e.kind = p.ExtendedPayload.Code
	}
	if e.detail == "" {
		e.detail = p.ExtendedPayload.Reason
	}
	if e.kind == "" && status == http.StatusTooManyRequests {
		e.kind = "RATE_LIMIT"
	}
	if e.detail == "" {
		e.detail = strings.TrimSpace(string(body))
		if len(e.detail) > 120 {
			e.detail = e.detail[:120] + "…"
		}
	}
	return e
}

// rateWait returns how long the API asked to wait, from Retry-After or the
// rate-limit headers.
func rateWait(status int, h http.Header) time.Duration {
	if s := h.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(s); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	if status == http.StatusTooManyRequests {
		return time.Hour
	}
	return 0
}

// ---- installations ----

type installation struct {
	ID       string    `json:"id"`
	Gateways []gateway `json:"gateways"`
}

type gateway struct {
	Serial  string   `json:"serial"`
	Devices []device `json:"devices"`
}

type device struct {
	ID         string   `json:"id"`
	ModelID    string   `json:"modelId"`
	DeviceType string   `json:"deviceType"`
	Status     string   `json:"status"`
	Roles      []string `json:"roles"`
}

// installations reads the installations of the account.
func (v *vicare) installations(ctx context.Context) ([]installation, error) {
	body, status, _, err := v.apiGet(ctx, "/iot/v2/equipment/installations?includeGateways=true")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, problem(status, body)
	}
	var answer struct {
		Data []installation `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("invalid installations answer: %w", err)
	}
	if len(answer.Data) == 0 {
		return nil, errors.New("the account has no installation: connect the heating system in the ViCare app first")
	}
	return answer.Data, nil
}

// resolve fills the installation, gateway and device from the account and the
// configured values. A configured value wins; the rest is discovered. The
// model and the roles come along, because the heating curve depends on them.
func (v *vicare) resolve(ctx context.Context) (installation, gateway, device, error) {
	var none installation
	list, err := v.installations(ctx)
	if err != nil {
		return none, gateway{}, device{}, err
	}
	insts := list
	if v.installation != "" {
		insts = nil
		for _, i := range list {
			if i.ID == v.installation {
				insts = append(insts, i)
			}
		}
		if len(insts) == 0 {
			return none, gateway{}, device{}, fmt.Errorf("installation %q is not in the account", v.installation)
		}
	}
	if len(insts) > 1 {
		return none, gateway{}, device{}, errors.New("the account has several installations: set installation")
	}
	inst := insts[0]

	gws := inst.Gateways
	if v.gateway != "" {
		gws = nil
		for _, g := range inst.Gateways {
			if g.Serial == v.gateway {
				gws = append(gws, g)
			}
		}
		if len(gws) == 0 {
			return none, gateway{}, device{}, fmt.Errorf("gateway %q is not in installation %s", v.gateway, inst.ID)
		}
	}
	if len(gws) == 0 {
		return none, gateway{}, device{}, fmt.Errorf("installation %s has no gateway; is the heating system online?", inst.ID)
	}
	if len(gws) > 1 {
		return none, gateway{}, device{}, errors.New("the installation has several gateways: set gateway")
	}
	gw := gws[0]

	if v.device != "" {
		for _, d := range gw.Devices {
			if d.ID == v.device {
				return inst, gw, d, nil
			}
		}
		return none, gateway{}, device{}, fmt.Errorf("device %q is not on gateway %s", v.device, gw.Serial)
	}
	// Prefer the heat generator; a gateway also lists ventilation, room
	// control and repeaters.
	for _, d := range gw.Devices {
		if d.DeviceType == "heating" {
			return inst, gw, d, nil
		}
	}
	if len(gw.Devices) == 0 {
		return none, gateway{}, device{}, fmt.Errorf("gateway %s has no device", gw.Serial)
	}
	if len(gw.Devices) > 1 {
		return none, gateway{}, device{}, errors.New("the gateway has several devices and no heating: set device")
	}
	return inst, gw, gw.Devices[0], nil
}
