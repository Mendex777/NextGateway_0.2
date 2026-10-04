package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func backupDB(t *testing.T) {
	var e error
	db, e = sql.Open("sqlite3", filepath.Join(t.TempDir(), "backup.db")+"?_foreign_keys=on")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, e = db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);CREATE TABLE sources(id INTEGER PRIMARY KEY,name TEXT,url TEXT,headers TEXT,updated TEXT,error TEXT);CREATE TABLE nodes(id INTEGER PRIMARY KEY,source_id INTEGER REFERENCES sources(id),uri TEXT,name TEXT,host TEXT,port TEXT,transport TEXT,security TEXT);CREATE TABLE rules(id INTEGER PRIMARY KEY,priority INTEGER,name TEXT,kind TEXT,value TEXT,target TEXT);INSERT INTO sources VALUES(1,'private','https://example.test/token','{"User-Agent":"app"}','','');INSERT INTO nodes VALUES(3,1,'vless://secret@example.test:443','node','example.test','443','tcp','tls');INSERT INTO rules VALUES(9,1,'route','domain','domain:example.com','node:3');INSERT INTO settings VALUES('selected_node','3'),('device:192.168.1.56','{"Name":"phone"}');`)
	if e != nil {
		t.Fatal(e)
	}
}
func TestBackupRoundTripAndRejectedChecksum(t *testing.T) {
	backupDB(t)
	raw, e := exportBackup()
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("UPDATE sources SET name='changed'")
	if e = restoreBackup(raw); e != nil {
		t.Fatal(e)
	}
	restored, e := exportBackup()
	if e != nil {
		t.Fatal(e)
	}
	var a, b panelBackup
	json.Unmarshal(raw, &a)
	json.Unmarshal(restored, &b)
	if string(a.Data) != string(b.Data) {
		t.Fatal("round trip lost data")
	}
	a.SHA256 = "invalid"
	bad, _ := json.Marshal(a)
	if restoreBackup(bad) == nil {
		t.Fatal("corruption accepted")
	}
	after, _ := exportBackup()
	json.Unmarshal(after, &b)
	if string(a.Data) != string(b.Data) {
		t.Fatal("rejected backup changed database")
	}
}
func TestBackupInvalidReferenceRollsBack(t *testing.T) {
	backupDB(t)
	before, _ := exportBackup()
	db.Exec("UPDATE settings SET value='missing' WHERE key='selected_node'")
	bad, _ := exportBackup()
	restoreBackup(before)
	if restoreBackup(bad) == nil {
		t.Fatal("missing node accepted")
	}
	after, _ := exportBackup()
	var a, b panelBackup
	json.Unmarshal(before, &a)
	json.Unmarshal(after, &b)
	if string(a.Data) != string(b.Data) {
		t.Fatal("failed transaction damaged data")
	}
}

func TestBackupHTTPUpload(t *testing.T) {
	backupDB(t)
	raw, e := exportBackup()
	if e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, _ := form.CreateFormFile("backup", "backup.json")
	f.Write(raw)
	form.Close()
	r := httptest.NewRequest("POST", "http://panel/backup", &body)
	r.Header.Set("Origin", "http://panel")
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	backupHandler(w, r)
	if w.Code != 303 {
		t.Fatalf("upload failed: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "http://panel/backup", nil)
	r.Header.Set("Origin", "http://other")
	w = httptest.NewRecorder()
	backupHandler(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
}

func TestGroupBackupAndMissingGroupRollback(t *testing.T) {
	backupDB(t)
	db.Exec("INSERT INTO nodes SELECT 4,source_id,uri,name,host,port,transport,security FROM nodes WHERE id=3")
	saveGroups([]BalanceGroup{{ID: "1", Name: "Example", Nodes: []string{"3", "4"}, Interval: 30}})
	saveSetting("default_route", "group:1")
	db.Exec("UPDATE rules SET target='group:1'")
	before, e := exportBackup()
	if e != nil {
		t.Fatal(e)
	}
	if e = restoreBackup(before); e != nil {
		t.Fatal(e)
	}
	saveSetting("default_route", "group:2")
	bad, _ := exportBackup()
	if e = restoreBackup(before); e != nil {
		t.Fatal(e)
	}
	if e = restoreBackup(bad); e == nil {
		t.Fatal("missing group accepted")
	}
	after, _ := exportBackup()
	var a, b panelBackup
	json.Unmarshal(before, &a)
	json.Unmarshal(after, &b)
	if string(a.Data) != string(b.Data) {
		t.Fatal("group restore damaged data")
	}
}

func TestInvalidGroupPolicyRestoreRollsBack(t *testing.T) {
	backupDB(t)
	db.Exec("INSERT INTO nodes SELECT 4,source_id,uri,name,host,port,transport,security FROM nodes WHERE id=3")
	g := BalanceGroup{ID: "1", Name: "Example", Nodes: []string{"3", "4"}, Interval: 30, Mode: "threshold", ThresholdMS: 1000, Failures: 2, Cooldown: 60}
	saveGroups([]BalanceGroup{g})
	before, _ := exportBackup()
	g.Cooldown = -1
	saveGroups([]BalanceGroup{g})
	bad, _ := exportBackup()
	if e := restoreBackup(before); e != nil {
		t.Fatal(e)
	}
	if e := restoreBackup(bad); e == nil {
		t.Fatal("invalid threshold policy restored")
	}
	after, _ := exportBackup()
	var a, b panelBackup
	json.Unmarshal(before, &a)
	json.Unmarshal(after, &b)
	if string(a.Data) != string(b.Data) {
		t.Fatal("failed policy restore changed data")
	}
}
