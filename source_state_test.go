package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDisabledSubscriptionExcludedAndRestored(t *testing.T) {
	configDatabase(t)
	_, err := db.Exec(`ALTER TABLE nodes ADD COLUMN source_id INTEGER; CREATE TABLE sources(id INTEGER PRIMARY KEY,url TEXT); INSERT INTO sources VALUES(1,'https://one.test'),(2,'https://two.test'); INSERT INTO nodes VALUES(1,'vless://a4d22397-77f8-4e75-96c1-027304162e20@one.test:443?security=none',1),(2,'vless://a4d22397-77f8-4e75-96c1-027304162e20@two.test:443?security=none',2); INSERT INTO rules VALUES(1,1,'fixed','domain','domain:fixed.test','node:1');`)
	if err != nil {
		t.Fatal(err)
	}
	saveSetting("selected_node", "1")
	saveSetting("default_route", "proxy")
	if err = setSourceEnabled("1", false); err != nil {
		t.Fatal(err)
	}
	if err = startProbe("1", "tcp"); err == nil {
		t.Fatal("disabled node probed")
	}
	if err = validateRuleNode("node:1"); err == nil {
		t.Fatal("disabled node assignable")
	}
	config, err := buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(config)
	if strings.Contains(string(raw), "one.test") || strings.Contains(string(raw), `"tag":"proxy"`) {
		t.Fatal("disabled node in Xray configuration")
	}
	if effectiveTarget("proxy") != "block" || effectiveTarget("node:1") != "block" {
		t.Fatal("disabled exit leaks to another route")
	}
	saveSetting("balance_groups", `[{"id":"1","name":"group","nodes":["1","2"],"interval":30}]`)
	saveSetting("default_route", "group:1")
	config, err = buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(config)
	if strings.Contains(string(raw), "one.test") || !strings.Contains(string(raw), "two.test") {
		t.Fatal("disabled participant not filtered")
	}
	setSourceEnabled("2", false)
	if effectiveTarget("group:1") != "block" {
		t.Fatal("empty group not blocked")
	}
	if _, err = buildConfig(); err != nil {
		t.Fatal(err)
	}
	setSourceEnabled("1", true)
	if setting("selected_node") != "1" || effectiveTarget("node:1") != "node:1" {
		t.Fatal("references not restored")
	}
}
