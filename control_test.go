package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func configDatabase(t *testing.T) {
	t.Helper()
	var e error
	db, e = sql.Open("sqlite3", filepath.Join(t.TempDir(), "config.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	_, e = db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);CREATE TABLE nodes(id INTEGER PRIMARY KEY,uri TEXT);CREATE TABLE rules(id INTEGER PRIMARY KEY,priority INTEGER,name TEXT,kind TEXT,value TEXT,target TEXT);INSERT INTO settings VALUES('default_route','direct'),('dns_direct','1.1.1.1'),('dns_mode','direct'),('selected_node','');`)
	if e != nil {
		t.Fatal(e)
	}
	if e := saveNetwork(GatewayNetwork{"ens18", "192.168.1.84", "192.168.1.0/24", "192.168.1.1", "", ""}); e != nil {
		t.Fatal(e)
	}
}
func TestProxyRequiresSelectedNode(t *testing.T) {
	configDatabase(t)
	if e := saveSetting("default_route", "proxy"); e != nil {
		t.Fatal(e)
	}
	if _, e := buildConfig(); e != nil {
		t.Fatal(e)
	}
	saveSetting("default_route", "direct")
	db.Exec("INSERT INTO rules VALUES(1,1,'proxy','domain','domain:example.com','proxy')")
	if _, e := buildConfig(); e != nil {
		t.Fatal(e)
	}
}
func TestRulePriorityAndNoBalancerFallback(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO nodes VALUES(1,'vless://fixture@127.0.0.1:1?security=none')")
	saveSetting("selected_node", "1")
	db.Exec("INSERT INTO rules VALUES(1,200,'later','domain','domain:later.test','direct'),(2,10,'earlier','domain','domain:earlier.test','proxy')")
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	rules := c["routing"].(map[string]any)["rules"].([]any)
	first := rules[4].(map[string]any)
	if first["outboundTag"] != "proxy" || first["domain"].([]string)[0] != "domain:earlier.test" {
		t.Fatal("wrong rule priority")
	}
	for _, o := range c["outbounds"].([]any) {
		out := o.(map[string]any)
		if out["tag"] == "proxy" && out["protocol"] != "vless" {
			t.Fatal("proxy has direct fallback")
		}
	}
	b, _ := json.Marshal(c)
	if !json.Valid(b) {
		t.Fatal("invalid JSON")
	}
}
func TestVLESSConversionRejectsUnsupportedCombinations(t *testing.T) {
	for _, uri := range []string{"vless://id@example.com:70000", "vless://id@[::1]:443", "vless://id@example.com:443?type=ws&security=reality&pbk=fixture", "vless://id@example.com:443?type=grpc&flow=xtls-rprx-vision", "vless://id@example.com:443?type=unknown"} {
		if _, e := vlessOutbound(uri); e == nil {
			t.Fatalf("unsupported URI accepted: %s", uri)
		}
	}
}
func TestDNSProxyBootstrap(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO nodes VALUES(1,'vless://fixture@proxy.example.com:443?security=tls')")
	saveSetting("selected_node", "1")
	saveSetting("dns_mode", "proxy")
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	servers := c["dns"].(map[string]any)["servers"].([]any)
	if len(servers) != 2 || servers[0].(map[string]any)["tag"] != "dns-bootstrap" || servers[1].(map[string]any)["tag"] != "dns-upstream" {
		t.Fatal("DNS proxy bootstrap resolver missing")
	}
}
