package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoutingTemplatePortableAndAtomic(t *testing.T) {
	backupDB(t)
	saveSetting("default_route", "proxy")
	saveSetting("rule_disabled:9", "1")
	saveSetting("rule_source:9", "192.168.1.56")
	template := exportRoutes()
	raw, _ := json.Marshal(template)
	if strings.Contains(string(raw), "vless://") || strings.Contains(string(raw), "https://example.test/token") || strings.Contains(string(raw), "node:3") {
		t.Fatal("template leaked credentials or local IDs")
	}
	if len(template.Exits) != 1 || template.Rules[0].Target != template.Exits[0].Key {
		t.Fatal("missing portable exit")
	}
	before := orderString(allRules())
	if _, e := importRoutes(template, nil, nil, true, false, before); e == nil {
		t.Fatal("unmapped exit accepted")
	}
	if orderString(allRules()) != before {
		t.Fatal("failed import modified rules")
	}
	mapping := map[string]string{template.Exits[0].Key: "block"}
	n, e := importRoutes(template, mapping, map[string]string{"192.168.1.56": "192.168.1.99"}, false, true, before)
	if e != nil || n != 1 {
		t.Fatalf("append: %d %v", n, e)
	}
	rules := allRules()
	if len(rules) != 2 || rules[1].Target != "block" || rules[1].Source != "192.168.1.99" || !rules[1].Disabled || setting("default_route") != "proxy" {
		t.Fatal("import lost state or mapping")
	}
	if _, e = importRoutes(template, mapping, nil, true, false, before); e == nil {
		t.Fatal("stale import accepted")
	}
	mapping[template.Exits[0].Key] = "skip"
	if n, e = importRoutes(template, mapping, nil, true, false, orderString(rules)); e != nil || n != 0 || len(allRules()) != 0 {
		t.Fatal("skip/replace failed")
	}
}
func TestGithubRoutingURLs(t *testing.T) {
	for _, raw := range []string{"http://github.com/a/b/blob/main/a.json", "https://127.0.0.1/a", "https://github.com:443/a/b/blob/main/a.json", "https://user@github.com/a/b/blob/main/a.json"} {
		if _, e := githubRouteURL(raw); e == nil {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	u, e := githubRouteURL("https://github.com/a/b/blob/main/routes.json?raw=true")
	if e != nil || u != "https://raw.githubusercontent.com/a/b/main/routes.json" {
		t.Fatal(u, e)
	}
}
func TestFullBackupSettingsAndAppliedSnapshot(t *testing.T) {
	backupDB(t)
	folder := t.TempDir()
	t.Setenv("NG_STATE", folder)
	snapshot := []byte(`{"outbounds":[{"tag":"direct","protocol":"freedom"}]}`)
	if e := os.WriteFile(filepath.Join(folder, "applied-config.json"), snapshot, 0600); e != nil {
		t.Fatal(e)
	}
	settings := map[string]string{"dns_mode": "rules", "dns_direct": "9.9.9.9", "gateway_enabled": "1", "vpn_unavailable": "direct", "source_disabled:1": "1", "rule_source:9": "192.168.1.56", "rule_disabled:9": "1", "node_order:3": "7", "sub_interval:1": "3600"}
	for k, v := range settings {
		saveSetting(k, v)
	}
	raw, e := exportBackup()
	if e != nil {
		t.Fatal(e)
	}
	var backup panelBackup
	json.Unmarshal(raw, &backup)
	if string(backup.AppliedConfig) != string(snapshot) {
		t.Fatal("applied snapshot missing")
	}
	db.Exec("DELETE FROM settings")
	if e = restoreBackup(raw); e != nil {
		t.Fatal(e)
	}
	for k, v := range settings {
		if setting(k) != v {
			t.Fatal("lost setting", k)
		}
	}
}

func TestImportDefaultRouteCheckbox(t *testing.T) {
	backupDB(t)
	saveSetting("default_route", "proxy")
	template := routeTemplate{Format: "ngpanel-routing", Version: 1, Default: "direct"}
	if _, err := importRoutes(template, nil, nil, false, false, orderString(allRules())); err != nil {
		t.Fatal(err)
	}
	if setting("default_route") != "proxy" {
		t.Fatal("unchecked checkbox changed default")
	}
	if _, err := importRoutes(template, nil, nil, false, true, orderString(allRules())); err != nil {
		t.Fatal(err)
	}
	if setting("default_route") != "direct" {
		t.Fatal("checked checkbox failed to change default")
	}
}
