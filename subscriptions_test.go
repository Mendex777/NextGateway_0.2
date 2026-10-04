package main

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionRefreshKeepsSelectionOnRenameAndRemoval(t *testing.T) {
	var e error
	db, e = sql.Open("sqlite3", filepath.Join(t.TempDir(), "sub.db"))
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE rules(id INTEGER PRIMARY KEY,target TEXT);CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);CREATE TABLE sources(id INTEGER PRIMARY KEY,name TEXT,url TEXT,headers TEXT,updated TEXT DEFAULT '',error TEXT DEFAULT '');CREATE TABLE nodes(id INTEGER PRIMARY KEY,source_id INTEGER,uri TEXT,name TEXT,host TEXT,port TEXT,transport TEXT,security TEXT);`)
	if e != nil {
		t.Fatal(e)
	}
	body := "vless://id@proxy.example.com:443?type=tcp&security=none#old"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}))
	defer server.Close()
	db.Exec("INSERT INTO sources(id,name,url,headers) VALUES(1,'test',?,'{}')", server.URL)
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	var id int
	db.QueryRow("SELECT id FROM nodes").Scan(&id)
	db.Exec("INSERT INTO rules VALUES(1,?)", "node:"+strconv.Itoa(id))
	saveSetting("selected_node", strconv.Itoa(id))
	body = "vless://id@proxy.example.com:443?security=none&type=tcp#renamed"
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	var sameID int
	var name string
	db.QueryRow("SELECT id,name FROM nodes").Scan(&sameID, &name)
	if id != sameID || name != "renamed" {
		t.Fatal("rename or query order lost node identity")
	}
	body = "vless://new@another.example.com:443?security=none#new"
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	db.QueryRow("SELECT name FROM nodes WHERE id=?", id).Scan(&name)
	if !strings.Contains(name, "исчез") || setting("selected_node") != strconv.Itoa(id) {
		t.Fatal("missing selected node removed or switched")
	}
	request := httptest.NewRequest("POST", "/action", strings.NewReader(url.Values{"action": {"source-update"}, "id": {"1"}, "name": {"changed"}, "url": {server.URL}, "headers": {"{}"}, "interval": {"15"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.ParseForm()
	if e = saveSource(request); e != nil {
		t.Fatal(e)
	}
	if setting("sub_interval:1") != "15" {
		t.Fatal("schedule not saved")
	}
	request.Form.Set("interval", "1")
	request.Form.Set("name", "invalid")
	if e = saveSource(request); e == nil {
		t.Fatal("invalid schedule accepted")
	}
	db.QueryRow("SELECT name FROM sources WHERE id=1").Scan(&name)
	if name != "changed" {
		t.Fatal("failed edit changed source")
	}
	before := requests
	refreshDueSources(time.Now())
	if requests != before+1 {
		t.Fatal("scheduled refresh did not run")
	}
	refreshDueSources(time.Now())
	if requests != before+1 {
		t.Fatal("schedule ignored refresh interval")
	}
	saveSetting("selected_node", "")
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	var retained int
	if e = db.QueryRow("SELECT id FROM nodes WHERE id=?", id).Scan(&retained); e != nil {
		t.Fatal("rule-bound node was removed when not globally selected")
	}

}
