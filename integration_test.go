package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshFailurePreservesNodes(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE sources(id INTEGER PRIMARY KEY,url TEXT,headers TEXT,updated TEXT,error TEXT); CREATE TABLE nodes(id INTEGER PRIMARY KEY,source_id INTEGER,uri TEXT,name TEXT,host TEXT,port TEXT,transport TEXT,security TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	good := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-HWID") != "fixture" {
			t.Error("custom header missing")
		}
		if good {
			w.Header().Set("Subscription-Userinfo", "upload=10; download=20; total=100; expire=1900000000")
			w.Write([]byte("vless://fixture@example.com:443?security=reality#Fixture"))
		} else {
			w.Write([]byte("<html>Expired subscription</html>"))
		}
	}))
	defer server.Close()
	if _, err = db.Exec(`INSERT INTO sources(id,url,headers) VALUES(1,?,?)`, server.URL, `{"X-HWID":"fixture"}`); err != nil {
		t.Fatal(err)
	}
	if err = refresh("1"); err != nil {
		t.Fatal(err)
	}
	before := setting("sub_info:1")
	if before == "" {
		t.Fatal("metadata not saved")
	}
	good = false
	if err = refresh("1"); err == nil {
		t.Fatal("expected failed refresh")
	}
	if setting("sub_info:1") != before {
		t.Fatal("failed refresh lost metadata")
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM nodes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("lost previous nodes: count=%d error=%v", count, err)
	}
}

func TestOriginGuard(t *testing.T) {
	for _, origin := range []string{"", "http://other.example"} {
		req := httptest.NewRequest("POST", "http://panel.example/action", strings.NewReader("action=settings&mode=direct"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != 403 {
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
}
