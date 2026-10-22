package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func healthyEvidence() healthEvidence {
	config := []byte(`{"inbounds":[{"tag":"tproxy-in","protocol":"dokodemo-door","port":7895,"settings":{"network":"tcp,udp","followRedirect":true},"streamSettings":{"sockopt":{"tproxy":"tproxy"}}},{"tag":"dns-in","port":1053}],"dns":{"servers":["192.168.1.1"]},"routing":{"rules":[{"domain":["geosite:google"],"ip":["geoip:private"]}]}}`)
	nft := []byte(`{"nftables":[{"table":{"family":"inet","name":"ngpanel","handle":1}},{"chain":{"family":"inet","table":"ngpanel","name":"prerouting","type":"filter","hook":"prerouting","prio":-150}},{"rule":{"family":"inet","table":"ngpanel","chain":"prerouting","handle":3,"expr":[{"counter":{"packets":20,"bytes":1000}},{"tproxy":{"port":7895}}]}}]}`)
	return healthEvidence{Runtime: Runtime{Gateway: true, ConfigHash: healthHash(config), AppliedNetwork: GatewayNetwork{Interface: "eth0", Address: "192.168.1.84", CIDR: "192.168.1.0/24", Router: "192.168.1.1"}}, Config: config, NFT: nft, Baseline: nft, Addresses: []byte(`[{"ifname":"eth0","flags":["UP"],"addr_info":[{"local":"192.168.1.84","prefixlen":24}]}]`), Routes: []byte(`[{"dst":"default","gateway":"192.168.1.1","dev":"eth0"}]`), Rules: []byte(`[{"priority":11000,"fwmark":"0x1","table":"100"}]`), LocalRoutes: []byte(`[{"type":"local","dst":"default","dev":"lo"}]`), Active: "active", Version: "Xray 26.3.27", Listeners: "*:7895 *:*\n*:1053 *:*", Forward: "1", Filter: "2", InterfaceFilter: "2", Autostart: "enabled\nenabled", ConfigValid: true, ConfigTested: time.Now(), DNSOK: true, Geo: map[string]string{"geosite.dat": "12 MB", "geoip.dat": "8 MB"}}
}
func checkState(t *testing.T, h ServiceHealth, id, want string) {
	t.Helper()
	for _, c := range h.Checks {
		if c.ID == id {
			if c.State != want {
				t.Fatalf("%s: %s (%s), want %s", id, c.State, c.Summary, want)
			}
			return
		}
	}
	t.Fatal("missing check", id)
}
func TestServiceHealthReady(t *testing.T) {
	h := buildServiceHealth(healthyEvidence())
	if len(h.Checks) != 10 {
		t.Fatal(len(h.Checks))
	}
	for _, c := range h.Checks {
		if c.State != "good" {
			t.Errorf("%s: %s %s", c.ID, c.State, c.Summary)
		}
	}
}
func TestServiceHealthDetectsFailures(t *testing.T) {
	cases := []struct {
		name, id, state string
		change          func(*healthEvidence)
	}{
		{"stopped", "xray", "bad", func(e *healthEvidence) { e.Active = "inactive" }},
		{"edited config", "config", "bad", func(e *healthEvidence) {
			e.Config = []byte(strings.Replace(string(e.Config), "google", "youtube", 1))
		}},
		{"invalid geo category", "config", "bad", func(e *healthEvidence) { e.ConfigValid = false }},
		{"missing geo", "geo", "bad", func(e *healthEvidence) { delete(e.Geo, "geosite.dat") }},
		{"DNS timeout", "dns", "bad", func(e *healthEvidence) { e.DNSOK = false }},
		{"missing policy", "routing", "bad", func(e *healthEvidence) { e.Rules = []byte(`[]`) }},
		{"wrong mask", "network", "bad", func(e *healthEvidence) { e.Addresses = []byte(strings.Replace(string(e.Addresses), "24", "16", 1)) }},
		{"forward disabled", "kernel", "bad", func(e *healthEvidence) { e.Forward = "0" }},
		{"missing listener", "listeners", "bad", func(e *healthEvidence) { e.Listeners = "" }},
		{"missing baseline", "nft", "warn", func(e *healthEvidence) { e.Baseline = nil }},
		{"changed rule", "nft", "bad", func(e *healthEvidence) { e.NFT = []byte(strings.Replace(string(e.NFT), "7895", "7896", 1)) }},
		{"autostart disabled", "autostart", "warn", func(e *healthEvidence) { e.Autostart = "enabled\ndisabled" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := healthyEvidence()
			tt.change(&e)
			checkState(t, buildServiceHealth(e), tt.id, tt.state)
		})
	}
}
func TestHealthNFTCounterChangesIgnored(t *testing.T) {
	e := healthyEvidence()
	changed := strings.ReplaceAll(strings.ReplaceAll(string(e.NFT), `"handle":3`, `"handle":900`), `"bytes":1000`, `"bytes":99999`)
	if canonicalHealthNFT([]byte(changed)) != canonicalHealthNFT(e.NFT) {
		t.Fatal("live counters affected baseline")
	}
	if canonicalHealthNFT([]byte(`{"nftables":[]}`)) != "" {
		t.Fatal("empty table accepted")
	}
}
func TestHealthStaleAndMissingReports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_REVIEW", "1")
	t.Setenv("NG_STATE", dir)
	if len(readServiceHealth().Checks) != 0 {
		t.Fatal("missing report accepted")
	}
	h := buildServiceHealth(healthyEvidence())
	h.Updated = time.Now().Add(-time.Minute)
	b, _ := json.Marshal(h)
	if err := os.WriteFile(filepath.Join(dir, "health.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	got := readServiceHealth()
	if got.Message == "" {
		t.Fatal("stale report not explained")
	}
	for _, c := range got.Checks {
		if c.State != "unknown" {
			t.Fatal("stale green status", c)
		}
	}
}
