package main

import (
	"testing"
	"time"
)

func TestConnectionAppliedRuleLabels(t *testing.T) {
	at := time.Unix(100, 0)
	r := domainResolver{}
	events := []ConnectionEvent{domainAuditEvent(t, "a", "p", "192.168.1.56:1234", "203.0.113.1:443", at), domainAuditEvent(t, "b", "p", "192.168.1.56:1235", "203.0.113.2:443", at)}
	for i, name := range []string{"Видео [личное]", "Другой маршрут"} {
		source := events[i].Source + ":" + events[i].SourcePort
		id := []string{"1", "2"}[i]
		r.observe("[Info] ["+id+"] proxy/dokodemo: received request for "+source, "p", at)
		tag := connectionRuleTag(Rule{ID: i + 1, Name: name})
		r.observe("[Info] ["+id+"] app/dispatcher: Hit route rule: ["+tag+"] so taking detour [direct] for [tcp:"+events[i].Destination+":443]", "p", at)
	}
	r.enrich(events, at)
	if events[0].Rule != "Видео [личное]" || events[1].Rule != "Другой маршрут" || events[0].Domain != "" {
		t.Fatalf("bad applied rules: %#v", events)
	}
	r.enrich(events, at.Add(10*time.Second))
	if events[0].Rule != "Видео [личное]" {
		t.Fatal("lost historical rule name")
	}
	// Same source tuple is ambiguous: never assign a guessed rule.
	r = domainResolver{}
	r.observe("[Info] [3] proxy/dokodemo: received request for 192.168.1.56:1234", "p", at)
	r.observe("[Info] [3] app/dispatcher: default route for tcp:203.0.113.1:443", "p", at)
	events = events[:1]
	r.enrich(events, at)
	if events[0].Rule != "Маршрут по умолчанию" {
		t.Fatal("missing default route")
	}
	duplicate := events[0]
	events = append(events, duplicate)
	r.enrich(events, at)
	if events[0].Rule != "" || events[1].Rule != "" {
		t.Fatal("guessed rule for ambiguous records")
	}
	if connectionRuleLabel("invalid") != "" || connectionRuleLabel("ngpanel-rule:0:YWJj") != "" {
		t.Fatal("accepted malformed tag")
	}
}

func TestConfigHasAppliedRuleNames(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO rules VALUES(1,1,'Видео','domain','domain:example.com','direct')")
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	rules := c["routing"].(map[string]any)["rules"].([]any)
	if connectionRuleLabel(rules[4].(map[string]any)["ruleTag"].(string)) != "Видео" {
		t.Fatal("user rule missing name")
	}
	if connectionRuleLabel(rules[len(rules)-1].(map[string]any)["ruleTag"].(string)) != "Маршрут по умолчанию" {
		t.Fatal("default rule missing name")
	}
}
