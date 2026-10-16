package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeQueuesFixedCollectorJob(t *testing.T) {
	configDatabase(t)
	root := t.TempDir()
	t.Setenv("NG_STATE", root)
	os.Mkdir(filepath.Join(root, "jobs"), 0700)
	submit := func() error { return enqueue("connections", nil) }
	if e := queueConnectionCollector("not-found", submit); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "jobs", "control.request"))
	if e != nil {
		t.Fatal(e)
	}
	var job Job
	if e = json.Unmarshal(raw, &job); e != nil {
		t.Fatal(e)
	}
	if job.Action != "connections" || job.Config != nil {
		t.Fatalf("unexpected migration job: %#v", job)
	}
	if e := queueConnectionCollector("loaded", func() error { t.Fatal("requeued setup for existing service"); return nil }); e != nil {
		t.Fatal(e)
	}
	if e := queueConnectionCollector("masked", func() error { t.Fatal("overrode administrator service mask"); return nil }); e == nil {
		t.Fatal("ignored masked service")
	}
}
