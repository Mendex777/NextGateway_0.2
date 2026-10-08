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
	body := "vless://id@proxy.example.com:443?type=tcp&security=reality&sid=aa&spx=%2F#old"
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
	body = "vless://id@proxy.example.com:443?security=reality&type=tcp&sid=bb&spx=%2Fnew#renamed"
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	var sameID int
	var name string
	db.QueryRow("SELECT id,name FROM nodes").Scan(&sameID, &name)
	if id != sameID || name != "renamed" {
		t.Fatal("rename or query order lost node identity")
	}
	// A historical duplicate referenced by a rule must merge into the selected ID.
	res, err := db.Exec("INSERT INTO nodes SELECT NULL,source_id,uri,name,host,port,transport,security FROM nodes WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _ := res.LastInsertId()
	db.Exec("INSERT INTO rules VALUES(2,?)", "node:"+strconv.FormatInt(duplicate, 10))
	saveSetting("default_route", "node:"+strconv.FormatInt(duplicate, 10))
	saveSetting("balance_groups", `[{"id":"test","name":"test","nodes":["`+strconv.FormatInt(duplicate, 10)+`"]}]`)
	if e = refresh("1"); e != nil {
		t.Fatal(e)
	}
	var count int
	db.QueryRow("SELECT count(*) FROM nodes").Scan(&count)
	var target string
	db.QueryRow("SELECT target FROM rules WHERE id=2").Scan(&target)
	if count != 1 || target != "node:"+strconv.Itoa(id) || setting("default_route") != target || balanceGroups()[0].Nodes[0] != strconv.Itoa(id) {
		t.Fatal("duplicate merge lost references")
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

func TestSubscriptionIdentityKeepsAuthenticationAndTransportDistinct(t *testing.T) {
	base := "vless://id@proxy.example.com:443?security=reality&type=tcp&pbk=key&sid=aa&spx=%2F"
	for _, raw := range []string{strings.Replace(base, "id@", "other@", 1), strings.Replace(base, "pbk=key", "pbk=other", 1), strings.Replace(base, "type=tcp", "type=grpc", 1)} {
		if canonicalURI(base) == canonicalURI(raw) {
			t.Fatal("distinct connection merged")
		}
	}
	if canonicalURI(base) != canonicalURI(strings.Replace(base, "sid=aa&spx=%2F", "sid=bb&spx=%2Fnew", 1)) {
		t.Fatal("rotating Reality parameters changed identity")
	}
}

func TestSubscriptionPreservesProviderOrderAfterRefresh(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "order.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE rules(id INTEGER PRIMARY KEY,target TEXT);CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);CREATE TABLE sources(id INTEGER PRIMARY KEY,name TEXT,url TEXT,headers TEXT,updated TEXT,error TEXT);CREATE TABLE nodes(id INTEGER PRIMARY KEY,source_id INTEGER,uri TEXT,name TEXT,host TEXT,port TEXT,transport TEXT,security TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	z := "vless://id@z.test:443?security=none#Zulu"
	a := "vless://id@a.test:443?security=none#Alpha"
	body := z + "\n" + a + "\n" + z
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}))
	defer server.Close()
	db.Exec("INSERT INTO sources VALUES(1,'test',?,'{}','','')", server.URL)
	for pass := 0; pass < 2; pass++ {
		if err = refresh("1"); err != nil {
			t.Fatal(err)
		}
		nodes := allNodes()
		if len(nodes) != 2 {
			t.Fatalf("unexpected nodes: %d", len(nodes))
		}
		want := []string{"Zulu", "Alpha"}
		if pass == 1 {
			want = []string{"Alpha", "Zulu"}
		}
		for i, n := range nodes {
			if n.Name != want[i] {
				t.Fatalf("pass %d: got %s at %d, want %s", pass, n.Name, i, want[i])
			}
		}
		if pass == 0 {
			saveSetting("selected_node", strconv.Itoa(nodes[0].ID))
			body = a + "\n" + z
		}
		if pass == 1 && setting("selected_node") != strconv.Itoa(nodes[1].ID) {
			t.Fatal("reorder lost selected node identity")
		}
	}
}
