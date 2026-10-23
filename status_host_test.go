package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthValidationSchedule(t *testing.T) {
	now := time.Now()
	c := healthCollector{signature: "same", valid: true, tested: now}
	if c.validationDue("same", now.Add(time.Minute)) {
		t.Fatal("unchanged valid config retested after one minute")
	}
	if !c.validationDue("same", now.Add(5*time.Minute)) {
		t.Fatal("periodic control check missing")
	}
	if !c.validationDue("changed", now.Add(time.Second)) {
		t.Fatal("changed input not checked immediately")
	}
	c.valid = false
	if c.validationDue("same", now.Add(30*time.Second)) || !c.validationDue("same", now.Add(time.Minute)) {
		t.Fatal("failed checks must retry in a minute")
	}
	c.tested = time.Time{}
	if !c.validationDue("same", now) {
		t.Fatal("initial check skipped")
	}
}

func TestHealthValidationSignatureTracksFilesAndIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	signature := func(config, account string) string {
		return healthValidationSignature([]byte(config), "Xray version", account, []string{path})
	}
	missing := signature("invalid A", "10:10")
	if err := os.WriteFile(path, []byte("core or geo"), 0600); err != nil {
		t.Fatal(err)
	}
	original := signature("invalid A", "10:10")
	if original == missing {
		t.Fatal("new input not detected")
	}
	if original == signature("invalid B", "10:10") || original == signature("invalid A", "11:10") {
		t.Fatal("config or service identity change missed")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	permissions := signature("invalid A", "10:10")
	if original == permissions {
		t.Fatal("permission change missed")
	}
	replacement := filepath.Join(dir, "replacement")
	os.WriteFile(replacement, []byte("replacement"), 0644)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if permissions == signature("invalid A", "10:10") {
		t.Fatal("core/geo replacement missed")
	}
	os.Remove(path)
	if missing != signature("invalid A", "10:10") {
		t.Fatal("removed input not detected")
	}
}

func TestHomeSnapshotFreshnessAndNetwork(t *testing.T) {
	addresses := []byte(`[{"ifname":"eth1","addr_info":[{"local":"10.1.0.2","prefixlen":24}]},{"ifname":"eth0","addr_info":[{"local":"192.168.1.84","prefixlen":24}]}]`)
	routes := []byte(`[{"dst":"default","dev":"eth1","gateway":"10.1.0.1","metric":100},{"dst":"default","dev":"eth0","gateway":"192.168.1.1","metric":10}]`)
	n, err := detectedHealthNetwork(addresses, routes)
	if err != nil || n.Address != "192.168.1.84" || n.CIDR != "192.168.1.0/24" {
		t.Fatal(n, err)
	}
	now := time.Now()
	h := ServiceHealth{Updated: now, Host: &statusHostSnapshot{Network: n}}
	if currentStatusHost(h, now) == nil {
		t.Fatal("fresh snapshot lost")
	}
	if currentStatusHost(h, now.Add(time.Minute)) != nil || currentStatusHost(h, now.Add(-time.Minute)) != nil {
		t.Fatal("stale or future snapshot accepted")
	}
	h.Host = nil
	if currentStatusHost(h, now) != nil {
		t.Fatal("old collector format accepted as a current snapshot")
	}
	if _, err = detectedHealthNetwork(addresses, []byte(`[]`)); err == nil {
		t.Fatal("missing route accepted")
	}
}

func TestHomeAndMetricsReadSnapshotWithoutCommands(t *testing.T) {
	backupDB(t)
	dir := t.TempDir()
	t.Setenv("NG_REVIEW", "1")
	t.Setenv("NG_STATE", dir)
	n := GatewayNetwork{Interface: "eth0", Address: "192.168.1.84", CIDR: "192.168.1.0/24", Router: "192.168.1.1"}
	if err := saveNetwork(n); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "commands")
	t.Setenv("NG_COMMAND_LOG", marker)
	t.Setenv("PATH", dir)
	for _, name := range []string{"ip", "systemctl", "xray", "free", "uptime"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s\n' \"$0\" >> \"$NG_COMMAND_LOG\"\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	host := &statusHostSnapshot{Network: n, Version: "Xray 26.10.21", Service: "active", XrayPID: os.Getpid(), ActiveSince: 1}
	h := ServiceHealth{Updated: time.Now(), Checks: []ServiceCheck{{ID: "xray", State: "good"}}, Host: host}
	raw, _ := json.Marshal(h)
	if err := os.WriteFile(filepath.Join(dir, "health.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	p := pageData(httptest.NewRequest("GET", "/?tab=status", nil))
	if p.Service != "active" || p.DetectedNetwork != n || p.Version != host.Version {
		t.Fatal("home did not use collector snapshot")
	}
	dashboardCache.Lock()
	dashboardCache.at = time.Time{}
	dashboardCache.Unlock()
	m := hostDashboard()
	if m.XrayPID != os.Getpid() || m.XrayMemory == 0 {
		t.Fatal("metrics did not use collector PID")
	}
	if raw, _ := os.ReadFile(marker); len(raw) > 0 {
		t.Fatalf("HTTP collection executed system commands: %s", raw)
	}
}
