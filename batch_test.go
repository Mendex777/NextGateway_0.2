package main

import (
	"database/sql"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchScopeAndCancellation(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE sources(id INTEGER PRIMARY KEY);CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);CREATE TABLE nodes(id INTEGER PRIMARY KEY,source_id INTEGER,uri TEXT,name TEXT,host TEXT,port TEXT,transport TEXT,security TEXT);INSERT INTO sources VALUES(1),(2);INSERT INTO nodes VALUES(1,1,'invalid','one','','','',''),(2,2,'invalid','other','','','','');`)
	if err != nil {
		t.Fatal(err)
	}
	saveSetting("selected_node", "2")
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if probeLock.TryLock() {
				probeLock.Unlock()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("probe did not release lock")
	}
	if err = startBatch("1"); err != nil {
		t.Fatal(err)
	}
	wait()
	batchMu.Lock()
	status := batchStatus
	batchMu.Unlock()
	if status.State != "done" || status.Total != 1 || status.Done != 1 || setting("node_probe:2") != "" || setting("selected_node") != "2" {
		t.Fatalf("batch changed other source or selection: %+v", status)
	}
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	db.Exec("UPDATE nodes SET uri=? WHERE id=1", "vless://11111111-1111-4111-8111-111111111111@"+server.Addr().String()+"?security=none")
	previous := ProbeResult{State: "ok", Message: "previous"}
	saveProbe("1", previous)
	if err = startBatch("1"); err != nil {
		t.Fatal(err)
	}
	cancelBatch()
	wait()
	batchMu.Lock()
	status = batchStatus
	batchMu.Unlock()
	var restored ProbeResult
	json.Unmarshal([]byte(setting("node_probe:1")), &restored)
	if status.State != "cancelled" || restored != previous {
		t.Fatalf("cancel overwrote previous result: %+v, %+v", status, restored)
	}
	if err = startBatch("missing"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err = startBatch("", "tcp"); err != nil {
		t.Fatal(err)
	}
	wait()
	batchMu.Lock()
	status = batchStatus
	batchMu.Unlock()
	if status.Total != 2 || status.Done != 2 || status.Mode != "tcp" || setting("node_probe:2") == "" || setting("selected_node") != "2" {
		t.Fatalf("global batch omitted nodes or changed selection: %+v", status)
	}
}
