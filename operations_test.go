package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationRejectsStaleResult(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	os.Mkdir(filepath.Join(dir, "jobs"), 0700)
	since := time.Now()
	s := Runtime{Action: "logs", State: "ok", Message: "old result", Updated: since.Add(-time.Minute).UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(s)
	os.WriteFile(filepath.Join(dir, "runtime.json"), b, 0600)
	if operationStatus("control", "logs", since).Done {
		t.Fatal("previous result reported as current")
	}
	s.Updated = since.Add(time.Second).UTC().Format(time.RFC3339Nano)
	s.Message = "current error"
	s.State = "error"
	b, _ = json.Marshal(s)
	os.WriteFile(filepath.Join(dir, "runtime.json"), b, 0600)
	result := operationStatus("control", "logs", since)
	if !result.Done || result.State != "error" || result.Message != s.Message {
		t.Fatal(result)
	}
	os.WriteFile(filepath.Join(dir, "jobs/control.request"), []byte("{}"), 0600)
	if operationStatus("control", "logs", since).Done {
		t.Fatal("reported completed while job pending")
	}
}

func TestCombinedNetworkActionTracksNetworkJob(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	since := time.Now().Add(-time.Second)
	raw, _ := json.Marshal(Runtime{Action: "network", State: "ok", Updated: time.Now().UTC().Format(time.RFC3339Nano)})
	os.WriteFile(filepath.Join(dir, "runtime.json"), raw, 0600)
	if operationKind("network-save-apply") != "control" || !operationStatus("control", "network-save-apply", since).Done {
		t.Fatal("combined network action did not track job completion")
	}
}
