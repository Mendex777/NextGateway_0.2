package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLightweightPollingViews(t *testing.T) {
	backupDB(t)
	t.Setenv("NG_REVIEW", "1")
	t.Setenv("NG_STATE", t.TempDir())
	if err := saveNetwork(GatewayNetwork{"ens18", "192.168.1.84", "192.168.1.0/24", "192.168.1.1", "", ""}); err != nil {
		t.Fatal(err)
	}
	saveSetting("device:192.168.1.56", `{"IP":"192.168.1.56","Name":"Phone","Manual":true}`)
	now := time.Now().UTC()
	snapshot := ConnectionSnapshot{Updated: now, TCPAvailable: true, Events: []ConnectionEvent{
		{ID: "active", Source: "192.168.1.56", Network: "tcp", Inbound: "tproxy-in", TCPState: "active", Outbound: "direct", Destination: "example.com", Port: "443"},
		{ID: "closed", Network: "tcp", Inbound: "tproxy-in", TCPState: "closed"},
		{ID: "udp", Network: "udp", Inbound: "tproxy-in"},
		{ID: "internal", Network: "tcp", Inbound: "test-socks", TCPState: "active"},
	}}
	write := func() {
		raw, _ := json.Marshal(snapshot)
		if err := os.WriteFile(filepath.Join(stateDir(), "connections.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	router := newRouter()
	get := func(path string) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		return w.Body.Bytes()
	}
	var active ConnectionSnapshot
	if json.Unmarshal(get("/api/connections?active=1"), &active) != nil || len(active.Events) != 1 || active.Events[0].ID != "active" || active.Events[0].Device != "Phone" || !active.TCPAvailable || !active.Updated.Equal(now) {
		t.Fatalf("incorrect active view: %#v", active)
	}
	var full ConnectionSnapshot
	json.Unmarshal(get("/api/connections"), &full)
	if len(full.Events) != 4 {
		t.Fatal("history lost")
	}
	raw, _ := json.Marshal(snapshot)
	if !bytes.Equal(get("/api/connections?raw=1"), raw) {
		t.Fatal("collector tuples changed")
	}
	snapshot.Updated = now.Add(-time.Minute)
	write()
	json.Unmarshal(get("/api/connections?active=1"), &active)
	if active.TCPAvailable {
		t.Fatal("stale TCP shown as live")
	}
	var devicesView struct{ Devices []Device }
	json.Unmarshal(get("/api/map/devices"), &devicesView)
	if len(devicesView.Devices) != 1 || devicesView.Devices[0].Name != "Phone" {
		t.Fatal("device name missing")
	}
	saveSetting("device:192.168.1.56", `{"IP":"192.168.1.56","Name":"Renamed","Manual":true}`)
	json.Unmarshal(get("/api/map/devices"), &devicesView)
	if devicesView.Devices[0].Name != "Renamed" {
		t.Fatal("device response cached stale name")
	}
	var settings Page
	json.Unmarshal(get("/api/page?tab=settings"), &settings)
	if settings.Tab != "settings" || settings.PanelVersion != panelVersion {
		t.Fatal("settings metadata missing")
	}
	saveSetting("node_probe:3", `{"State":"ok","HTTPSMS":123}`)
	var probes struct{ Nodes map[int]ProbeResult }
	json.Unmarshal(get("/probe-status"), &probes)
	if probes.Nodes[3].State != "ok" || probes.Nodes[3].HTTPSMS != 123 {
		t.Fatal("probe results changed")
	}
	saveSetting("node_probe:3", `{"State":"error","HTTPSMS":0}`)
	json.Unmarshal(get("/probe-status"), &probes)
	if probes.Nodes[3].State != "error" {
		t.Fatal("probe response cached stale health")
	}
}
