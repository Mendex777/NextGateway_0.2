package main

import (
	"net/url"
	"testing"
)

func TestOrderingPreservesRulesAndRejectsStaleLists(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO rules VALUES(1,100,'first','domain','domain:a.test','block'),(2,10,'second','domain','domain:b.test','direct'),(3,100,'third','domain','domain:c.test','block')")
	saveSetting("rule_disabled:1", "1")
	saveSetting("rule_source:1", "192.168.1.56")
	if orderString(allRules()) != "2,1,3" {
		t.Fatal("existing order changed")
	}
	if e := reorderRules("rule-order", "", "3,2,1", "2,1,3"); e != nil {
		t.Fatal(e)
	}
	if orderString(allRules()) != "3,2,1" {
		t.Fatal("drop not saved")
	}
	last := allRules()[2]
	if !last.Disabled || last.Source != "192.168.1.56" || last.Value != "domain:a.test" {
		t.Fatal("reorder lost data")
	}
	for _, order := range []string{"3,3,1", "3,2", "3,2,999"} {
		if e := reorderRules("rule-order", "", order, "3,2,1"); e == nil {
			t.Fatal("invalid list accepted")
		}
	}
	if e := reorderRules("rule-order", "", "2,1,3", "2,1,3"); e == nil {
		t.Fatal("stale order accepted")
	}
	if e := reorderRules("rule-up", "1", "", "3,2,1"); e != nil {
		t.Fatal(e)
	}
	if orderString(allRules()) != "3,1,2" {
		t.Fatal("up failed")
	}
	if e := ruleAction(t, url.Values{"action": {"rule-add"}, "name": {"new"}, "kind": {"domain"}, "value": {"new.test"}, "target": {"direct"}}); e != nil {
		t.Fatal(e)
	}
	if orderString(allRules()) != "3,1,2,4" {
		t.Fatal("new rule not appended")
	}
}
