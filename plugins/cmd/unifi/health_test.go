package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHealth(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "health.json"))
	if err != nil {
		t.Fatal(err)
	}
	wan, www, err := parseHealth(data)
	if err != nil {
		t.Fatalf("parseHealth: %v", err)
	}
	if wan.Status != "ok" {
		t.Errorf("wan.status = %q, want ok", wan.Status)
	}
	if wan.ISPName != "Deutsche Telekom AG" {
		t.Errorf("wan.isp_name = %q", wan.ISPName)
	}
	if wan.WANIP != "203.0.113.9" {
		t.Errorf("wan.wan_ip = %q", wan.WANIP)
	}
	if wan.TxBytesR != 4961 || wan.RxBytesR != 4976 {
		t.Errorf("throughput = %v/%v, want 4961/4976", wan.TxBytesR, wan.RxBytesR)
	}
	if wan.NumSta != 74 {
		t.Errorf("wan.num_sta = %d, want 74", wan.NumSta)
	}
	if www.Status != "ok" || www.Latency == nil || *www.Latency != 10 || www.Drops == nil || *www.Drops != 1 {
		t.Errorf("www = %+v", www)
	}

	// The WAN3 entry at 0 percent is a standby interface, not the state of a
	// configured WAN: the snapshot reads only wan.status and www.status, so a
	// standby WAN does not mark the state degraded (issue #494).
	snap := snapshot{loaded: true, wan: wan, www: www}
	if got := snap.stateKey(true); got != "ok" {
		t.Errorf("state = %q, want ok", got)
	}
}

func TestParseHealthBroken(t *testing.T) {
	for name, data := range map[string][]byte{
		"rc":        []byte(`{"meta":{"rc":"error"},"data":[]}`),
		"no meta":   []byte(`{"data":[]}`),
		"no answer": []byte(`no json`),
	} {
		if _, _, err := parseHealth(data); err == nil {
			t.Errorf("%s: parseHealth accepted a broken answer", name)
		}
	}
}

func TestParseHealthMissingEntries(t *testing.T) {
	wan, www, err := parseHealth([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
	if err != nil {
		t.Fatalf("parseHealth: %v", err)
	}
	if wan.Status != "" || www.Status != "" {
		t.Errorf("missing entries = %q / %q, want empty", wan.Status, www.Status)
	}
}
