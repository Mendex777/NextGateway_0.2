package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardParsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc")
	os.WriteFile(path, []byte("MemTotal: 100 kB\nThreads: 12\ninvalid\n"), 0600)
	values := procNumbers(path)
	if values["MemTotal"] != 102400 || values["Threads"] != 12 {
		t.Fatal(values)
	}
	os.WriteFile(path, []byte("header\n first\n second\n"), 0600)
	if socketCount(path) != 2 {
		t.Fatal("socket count includes header")
	}
}
func TestDashboardHistoryWindow(t *testing.T) {
	dashboardHistory.Lock()
	dashboardHistory.samples = nil
	dashboardHistory.Unlock()
	for i := int64(1); i <= 80; i++ {
		recordDashboardSample(dashboardMetrics{Time: i, CPU: float64(i)})
	}
	values := readDashboardHistory()
	if len(values) != 72 || values[0].Time != 9 || values[71].Time != 80 {
		t.Fatal("history window lost order or bound")
	}
	recordDashboardSample(dashboardMetrics{Time: 80})
	values[0].Time = 0
	if next := readDashboardHistory(); len(next) != 72 || next[0].Time != 9 {
		t.Fatal("duplicate or aliased history")
	}
}
func TestCPUTopology(t *testing.T) {
	cores, mhz := cpuTopology("physical id : 0\ncore id : 0\ncpu MHz : 2000\n\nphysical id : 0\ncore id : 0\n\nphysical id : 0\ncore id : 1\n")
	if cores != 2 || mhz != 2000 {
		t.Fatal("CPU cores confused with threads")
	}
}
func TestDashboardMetricsAreReadOnlyAndCached(t *testing.T) {
	first := hostDashboard()
	second := hostDashboard()
	if first.Time != second.Time || first.MemoryTotal == 0 || first.Uptime <= 0 || first.CPU < 0 || first.CPU > 100 {
		t.Fatal("invalid host sample or duplicate request resampled counters")
	}
}
func TestDashboardConfigSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	if _, err := dashboardConfig(); err == nil {
		t.Fatal("missing config accepted")
	}
	os.WriteFile(filepath.Join(dir, "applied-config.json"), []byte(`{"routing":{"rules":[]}}`), 0600)
	raw, err := dashboardConfig()
	if err != nil || !json.Valid(raw) {
		t.Fatal("valid snapshot unavailable")
	}
	os.WriteFile(filepath.Join(dir, "applied-config.json"), []byte(`broken`), 0600)
	if _, err = dashboardConfig(); err == nil {
		t.Fatal("invalid config accepted")
	}
}
func TestDashboardLogsWhitelist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	os.WriteFile(filepath.Join(dir, "xray-log"), []byte("xray cached log"), 0600)
	os.WriteFile(filepath.Join(dir, "panel-log"), []byte("panel cached log"), 0600)
	for _, test := range []struct{ query, want string }{{"panel", "panel cached log"}, {"../../etc/passwd", "xray cached log"}} {
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/api/dashboard/logs?service="+test.query, nil))
		var value struct {
			Text string `json:"text"`
		}
		json.Unmarshal(w.Body.Bytes(), &value)
		if w.Code != 200 || value.Text != test.want {
			t.Fatal("logs selection escaped whitelist")
		}
	}
}
func TestRestoreKeepsMachineNetwork(t *testing.T) {
	backupDB(t)
	db.Exec("INSERT INTO settings VALUES('gateway_network','original')")
	raw, err := exportBackup()
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("UPDATE settings SET value='current' WHERE key='gateway_network'")
	if err = restoreBackupOptions(raw, true); err != nil {
		t.Fatal(err)
	}
	if setting("gateway_network") != "current" {
		t.Fatal("machine network overwritten")
	}
	if err = restoreBackupOptions(raw, false); err != nil {
		t.Fatal(err)
	}
	if setting("gateway_network") != "original" {
		t.Fatal("normal restore failed")
	}
}
