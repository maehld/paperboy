package main

import (
	"encoding/json"
	"fmt"
)

// healthResponse is the answer of the private controller endpoint
// /proxy/network/api/s/<site>/stat/health.
type healthResponse struct {
	Meta struct {
		RC string `json:"rc"`
	} `json:"meta"`
	Data []healthSubsystem `json:"data"`
}

// healthSubsystem is one entry of the health answer. The wan entry carries the
// WAN summary, the www entry the internet tests. uptime_stats is deliberately
// not parsed: its keys are interface names, not the configured WANs, and a
// standby WAN reports 0 percent, which must not mark the state degraded
// (issue #494). xput_up and xput_down are speedtest results; the live
// throughput is tx_bytes-r and rx_bytes-r.
type healthSubsystem struct {
	Subsystem string `json:"subsystem"`
	Status    string `json:"status"`
	// The wan entry.
	ISPName  string  `json:"isp_name"`
	WANIP    string  `json:"wan_ip"`
	TxBytesR float64 `json:"tx_bytes-r"`
	RxBytesR float64 `json:"rx_bytes-r"`
	NumSta   int     `json:"num_sta"`
	// The www entry.
	Latency *int64 `json:"latency"`
	Drops   *int64 `json:"drops"`
}

// parseHealth decodes a health answer and returns the wan and the www entry.
// A meta.rc other than ok is an error; a missing wan or www entry yields its
// zero value, so the caller treats it like an unread value.
func parseHealth(data []byte) (wan, www healthSubsystem, err error) {
	var h healthResponse
	if err := json.Unmarshal(data, &h); err != nil {
		return wan, www, fmt.Errorf("health: answer: %w", err)
	}
	if h.Meta.RC != "ok" {
		return wan, www, fmt.Errorf("health: meta.rc = %q, want ok", h.Meta.RC)
	}
	for _, s := range h.Data {
		switch s.Subsystem {
		case "wan":
			wan = s
		case "www":
			www = s
		}
	}
	return wan, www, nil
}
