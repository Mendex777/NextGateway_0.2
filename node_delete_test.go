package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeleteAllNodesKeepsRulesAndBackup(t *testing.T) {
	backupDB(t)
	db.Exec("INSERT INTO nodes SELECT 4,source_id,uri,name,host,port,transport,security FROM nodes WHERE id=3")
	saveSetting("balance_groups", `[{"id":"1","name":"Group","nodes":["3","4"],"interval":30}]`)
	saveSetting("default_route", "group:1")
	if err := deleteConnections("1", true); err != nil {
		t.Fatal(err)
	}
	if setting("selected_node") != "" {
		t.Fatal("selection not cleared")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM rules").Scan(&count)
	if count != 1 {
		t.Fatal("rules deleted")
	}
	raw, err := exportBackup()
	if err != nil {
		t.Fatal(err)
	}
	if err = restoreBackup(raw); err != nil {
		t.Fatal(err)
	}
	if effectiveTarget("node:3") != "block" || effectiveTarget("group:1") != "block" {
		t.Fatal("missing exits not blocked")
	}
	db.Exec("INSERT INTO sources VALUES(2,'new','https://new.test','{}','','')")
	_, err = db.Exec("INSERT INTO nodes(id,source_id,uri,name,host,port,transport,security) VALUES(" + nextNodeID + ",2,'vless://new@new.test:443','new','new.test','443','tcp','none')")
	if err != nil {
		t.Fatal(err)
	}
	var id int
	db.QueryRow("SELECT id FROM nodes").Scan(&id)
	if id <= 4 {
		t.Fatal("deleted node ID reused")
	}
	if effectiveTarget("node:3") != "block" {
		t.Fatal("old rule attached to new node")
	}
	saveSetting("vpn_unavailable", "direct")
	if effectiveTarget("node:3") != "direct" {
		t.Fatal("explicit direct fallback ignored")
	}
}

func TestMissingVPNBuildsBlockAndExplicitDirect(t *testing.T) {
	configDatabase(t)
	saveSetting("default_route", "proxy")
	saveSetting("dns_mode", "proxy")
	db.Exec("INSERT INTO rules VALUES(1,1,'VPN','domain','domain:example.test','proxy')")
	for _, want := range []string{"block", "direct"} {
		saveSetting("vpn_unavailable", want)
		config, err := buildConfig()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(config)
		if strings.Contains(string(raw), `"tag":"proxy"`) {
			t.Fatal("missing proxy emitted")
		}
		entries := config["routing"].(map[string]any)["rules"].([]any)
		if entries[len(entries)-1].(map[string]any)["outboundTag"] != want {
			t.Fatal("default route fallback incorrect")
		}
		if vpnAvailabilityWarning() == "" {
			t.Fatal("missing warning")
		}
	}
}
