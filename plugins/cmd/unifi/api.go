package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// controller is one HTTP client of the controller. The documented integration
// API and the private controller API share the base address, the X-API-KEY
// header and the envelope handling.
type controller struct {
	base string
	key  string
	http *http.Client
}

// integrationBase is the documented UniFi integration API on a UniFi OS
// console. The OpenAPI document calls it /integration/v1; the console serves
// it below /proxy/network.
const integrationBase = "/proxy/network/integration/v1"

// integrationAPI is the documented UniFi integration API (GET
// /proxy/network/integration/v1/...).
type integrationAPI struct{ controller }

// privateAPI is the controller's private API behind /proxy/network/api/. It
// answers the short WAN summary; its endpoints are not documented, so the
// parsing stays defensive.
type privateAPI struct{ controller }

// get reads one endpoint and returns the body. A non-200 answer is an error
// with the path and the status.
func (c *controller) get(ctx context.Context, path string) ([]byte, error) {
	client := c.http
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", c.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("%s: %s", path, msg)
	}
	return body, nil
}

// sites reads the sites of the controller.
func (c *integrationAPI) sites(ctx context.Context) ([]siteInfo, error) {
	body, err := c.get(ctx, integrationBase+"/sites")
	if err != nil {
		return nil, err
	}
	var page struct {
		Data []siteInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("%s/sites: answer: %w", integrationBase, err)
	}
	return page.Data, nil
}

// devicePageSize is one page of the device list. totalCount lets the plugin
// read past the first page, so a large site still counts every device.
const devicePageSize = 200

// devices reads every page of the device list of one site.
func (c *integrationAPI) devices(ctx context.Context, siteID string) ([]device, error) {
	var all []device
	for {
		path := fmt.Sprintf("%s/sites/%s/devices?limit=%d&offset=%d",
			integrationBase, siteID, devicePageSize, len(all))
		body, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		var page struct {
			Data       []device `json:"data"`
			TotalCount int      `json:"totalCount"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("devices: answer: %w", err)
		}
		all = append(all, page.Data...)
		if len(page.Data) == 0 || len(page.Data) < devicePageSize {
			break
		}
		// Without a totalCount the first full page is all the plugin can
		// trust, so it does not ask again.
		if page.TotalCount <= 0 || len(all) >= page.TotalCount {
			break
		}
	}
	return all, nil
}

// clients reads the client count of one site. The page is asked with limit 1
// and only its totalCount is used.
func (c *integrationAPI) clients(ctx context.Context, siteID string) (int, error) {
	body, err := c.get(ctx, integrationBase+"/sites/"+siteID+"/clients?limit=1")
	if err != nil {
		return 0, err
	}
	var page struct {
		TotalCount int `json:"totalCount"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return 0, fmt.Errorf("clients: answer: %w", err)
	}
	return page.TotalCount, nil
}

// health reads the short WAN summary from the private endpoint.
func (c *privateAPI) health(ctx context.Context, internalReference string) (wan, www healthSubsystem, err error) {
	body, err := c.get(ctx, "/proxy/network/api/s/"+internalReference+"/stat/health")
	if err != nil {
		return wan, www, err
	}
	return parseHealth(body)
}

// pickSite resolves the configured site: by name or id, or the first site
// without a configured one.
func pickSite(sites []siteInfo, want string) (siteInfo, error) {
	if len(sites) == 0 {
		return siteInfo{}, fmt.Errorf("no sites on the controller")
	}
	if want == "" {
		return sites[0], nil
	}
	for _, s := range sites {
		if s.ID == want || s.Name == want {
			return s, nil
		}
	}
	return siteInfo{}, fmt.Errorf("site %q: not found on the controller", want)
}
