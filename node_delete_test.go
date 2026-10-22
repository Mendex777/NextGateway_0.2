package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanupMissingSubscriptionNodes(t *testing.T) {
	backupDB(t)
	db.Exec("UPDATE nodes SET name=name || ? WHERE id=3", missingNodeSuffix)
	db.Exec("INSERT INTO nodes SELECT 4,source_id,uri,'current',host,port,transport,security FROM nodes WHERE id=3")
	db.Exec("INSERT INTO sources VALUES(2,'other','https://other.test','{}','','')")
	db.Exec("INSERT INTO nodes SELECT 5,2,uri,name,host,port,transport,security FROM nodes WHERE id=3")
	db.Exec("INSERT INTO nodes SELECT 6,1,uri,'renamed missing',host,port,transport,security FROM nodes WHERE id=3")
	saveSetting("node_missing:6", "1")
	saveSetting("balance_groups", `[{"id":"1","name":"group","nodes":["3","4"],"interval":30}]`)
	saveSetting("node_probe:3", `{}`)
	saveSetting("node_order:3", "1")
	saveSetting("group_preferred:1", "3")
	if _, err := deleteMissingConnections("1", "3"); err == nil {
		t.Fatal("stale confirmation accepted")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM nodes").Scan(&count)
	if count != 4 || setting("selected_node") != "3" {
		t.Fatal("rejected cleanup mutated database")
	}
	removed, err := deleteMissingConnections("1", "6,3")
	if err != nil || removed != 2 {
		t.Fatalf("cleanup: %d %v", removed, err)
	}
	db.QueryRow("SELECT count(*) FROM nodes WHERE id IN (4,5)").Scan(&count)
	groups := balanceGroups()
	if count != 2 || setting("selected_node") != "" || len(groups) != 1 || strings.Join(groups[0].Nodes, ",") != "4" || setting("group_preferred:1") != "" {
		t.Fatal("cleanup did not remove deleted group members safely")
	}
	db.QueryRow("SELECT count(*) FROM sources").Scan(&count)
	if count != 2 || setting("node_deleted:3") != "1" || setting("node_deleted:6") != "1" || setting("node_missing:6") != "" || setting("node_probe:3") != "" {
		t.Fatal("source lost or deletion metadata incorrect")
	}
	var target string
	db.QueryRow("SELECT target FROM rules WHERE id=9").Scan(&target)
	if target != "node:3" {
		t.Fatal("rule changed")
	}
	if _, err := deleteMissingConnections("1", "3,6"); err == nil {
		t.Fatal("repeated cleanup accepted")
	}
}

func TestRepairDeletedGroupMembersOnlyConfirmedAbsent(t *testing.T) {
	backupDB(t)
	saveSetting("balance_groups", `[{"id":"1","name":"empty","nodes":["7"],"interval":30,"cooldown":60},{"id":"2","name":"kept","nodes":["3","9","8"],"interval":30,"cooldown":60}]`)
	saveSetting("balance_settings", `{"enabled":true,"nodes":["7","3"],"interval":30}`)
	saveSetting("node_deleted:7", "1")
	saveSetting("node_deleted:3", "1") // Existing nodes must remain even if metadata is old.
	saveSetting("node_deleted:8", "0")
	if err := repairDeletedGroupMembers(); err != nil {
		t.Fatal(err)
	}
	groups := balanceGroups()
	if len(groups) != 2 || len(groups[0].Nodes) != 0 || strings.Join(groups[1].Nodes, ",") != "3,9,8" || strings.Join(balanceSettings().Nodes, ",") != "3" {
		t.Fatal("repair removed an unconfirmed member or lost group")
	}
	before := setting("balance_groups")
	if err := repairDeletedGroupMembers(); err != nil || setting("balance_groups") != before {
		t.Fatal("repair not idempotent", err)
	}
}

func TestCleanupRollsBackNodesWhenGroupInvalid(t *testing.T) {
	backupDB(t)
	db.Exec("UPDATE nodes SET name=name || ? WHERE id=3", missingNodeSuffix)
	saveSetting("balance_groups", `invalid`)
	if _, err := deleteMissingConnections("1", "3"); err == nil {
		t.Fatal("invalid group accepted")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM nodes WHERE id=3").Scan(&count)
	if count != 1 || setting("selected_node") != "3" || setting("node_deleted:3") != "" {
		t.Fatal("failed cleanup partially committed")
	}
}

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
