package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBalanceRoutesDNSAndKeepsFixedNodes(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO nodes VALUES(1,'vless://a4d22397-77f8-4e75-96c1-027304162e20@one.test:443?security=none'),(2,'vless://a4d22397-77f8-4e75-96c1-027304162e20@two.test:443?security=none')")
	saveSetting("selected_node", "1")
	saveSetting("dns_mode", "proxy")
	db.Exec("INSERT INTO rules VALUES(1,1,'auto','domain','domain:auto.test','proxy'),(2,2,'fixed','domain','domain:fixed.test','node:2')")
	raw, _ := json.Marshal(BalanceSettings{Enabled: true, Nodes: []string{"2"}, Interval: 30})
	saveSetting("balance_settings", string(raw))
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	routing := c["routing"].(map[string]any)
	balancers := routing["balancers"].([]any)
	if balancers[0].(map[string]any)["fallbackTag"] != "block" {
		t.Fatal("VPN can fall back to direct")
	}
	groupRules, fixed := 0, false
	for _, r := range routing["rules"].([]any) {
		entry := r.(map[string]any)
		if entry["outboundTag"] == "proxy" {
			t.Fatal("DNS or routing bypassed group")
		}
		if entry["balancerTag"] == "auto-vpn" {
			groupRules++
		}
		if entry["outboundTag"] == "node-2" {
			fixed = true
		}
	}
	if groupRules < 2 || !fixed {
		t.Fatal("group DNS or fixed rule missing")
	}
	tags := map[string]bool{}
	for _, o := range c["outbounds"].([]any) {
		tags[o.(map[string]any)["tag"].(string)] = true
	}
	if !tags["auto-vpn-1-"] || !tags["auto-vpn-2-"] || tags["proxy"] || !tags["node-2"] {
		t.Fatal("outbound group incorrect")
	}
	encoded, _ := json.Marshal(c["dns"])
	if !strings.Contains(string(encoded), "full:two.test") {
		t.Fatal("reserve hostname does not bootstrap directly")
	}
	if os.Getenv("NG_XRAY_INTEGRATION") == "1" {
		raw, _ := json.Marshal(c)
		path := filepath.Join(t.TempDir(), "config.json")
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("/usr/local/bin/xray", "run", "-test", "-c", path)
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("Xray rejected generated group: %v %s", e, output)
		}
		check := exec.Command("python3", "-c", `import importlib.util,json,sys,copy
s=importlib.util.spec_from_file_location('control','deploy/control.py');c=importlib.util.module_from_spec(s);s.loader.exec_module(c)
config=json.load(sys.stdin);c.validate(config)
for kind in ('api','fallback','probe'):
 bad=copy.deepcopy(config)
 if kind=='api':bad['api']['listen']='0.0.0.0:10085'
 elif kind=='fallback':bad['routing']['balancers'][0]['fallbackTag']='direct'
 else:bad['observatory']['probeUrl']='http://127.0.0.1:22'
 try:c.validate(bad)
 except ValueError:continue
 raise Exception('Unsafe '+kind+' accepted')
`)
		check.Stdin = bytes.NewReader(raw)
		if output, e := check.CombinedOutput(); e != nil {
			t.Fatalf("Root validation: %v %s", e, output)
		}
	}

}

func TestBalanceRejectsMissingMemberAndLimits(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO nodes VALUES(1,'vless://a4d22397-77f8-4e75-96c1-027304162e20@one.test:443?security=none')")
	saveSetting("selected_node", "1")
	for _, b := range []BalanceSettings{
		{Enabled: true, Nodes: []string{"1"}, Interval: 30},
		{Enabled: true, Nodes: []string{"2"}, Interval: 30},
		{Enabled: true, Nodes: []string{"2"}, Interval: 1},
	} {
		raw, _ := json.Marshal(b)
		saveSetting("balance_settings", string(raw))
		if _, e := buildConfig(); e == nil {
			t.Fatal("invalid group accepted")
		}
	}
}
