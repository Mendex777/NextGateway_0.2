package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPanelUpdateQueueExclusive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	os.Mkdir(filepath.Join(dir, "jobs"), 0700)
	if e := requestPanelUpdate("check"); e != nil {
		t.Fatal(e)
	}
	if requestPanelUpdate("install") == nil {
		t.Fatal("overwrote running job")
	}
	b, e := os.ReadFile(filepath.Join(dir, "jobs/panel-update.request"))
	if e != nil {
		t.Fatal(e)
	}
	var job map[string]string
	if json.Unmarshal(b, &job) != nil || job["action"] != "check" {
		t.Fatal("incomplete or overwritten request")
	}
	if requestPanelUpdate("shell") == nil {
		t.Fatal("unknown action allowed")
	}
}
func TestPanelHealthChecksDB(t *testing.T) {
	configDatabase(t)
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "http://panel/health", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var data map[string]string
	json.Unmarshal(w.Body.Bytes(), &data)
	if data["version"] != panelVersion {
		t.Fatal("wrong health version")
	}
}
