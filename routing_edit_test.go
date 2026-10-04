package main

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func ruleAction(t *testing.T, values url.Values) error {
	t.Helper()
	r := httptest.NewRequest("POST", "/action", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ParseForm()
	handled, _, e := controlAction(r)
	if !handled {
		t.Fatal("action not handled")
	}
	return e
}
func TestRuleEditDisableAndReenable(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO rules VALUES(1,100,'vpn','domain','domain:example.com','proxy')")
	if _, e := buildConfig(); e == nil {
		t.Fatal("active proxy rule must require a node")
	}
	toggle := url.Values{"action": {"rule-toggle"}, "id": {"1"}}
	if e := ruleAction(t, toggle); e != nil {
		t.Fatal(e)
	}
	c, e := buildConfig()
	if e != nil {
		t.Fatal("disabled proxy still requires node:", e)
	}
	for _, r := range c["routing"].(map[string]any)["rules"].([]any) {
		if r.(map[string]any)["outboundTag"] == "proxy" {
			t.Fatal("disabled rule leaked into configuration")
		}
	}
	edit := url.Values{"action": {"rule-update"}, "id": {"1"}, "priority": {"5"}, "name": {"edited"}, "kind": {"domain"}, "value": {"new.example.com"}, "target": {"block"}}
	if e := ruleAction(t, edit); e != nil {
		t.Fatal(e)
	}
	rules := allRules()
	if len(rules) != 1 || !rules[0].Disabled || rules[0].Name != "edited" || rules[0].Priority != 100 {
		t.Fatal("edit lost disabled state")
	}
	edit.Set("kind", "ip")
	edit.Set("value", "::1")
	if e := ruleAction(t, edit); e == nil {
		t.Fatal("invalid edit accepted")
	}
	if allRules()[0].Value != "domain:new.example.com" {
		t.Fatal("invalid edit overwrote valid rule")
	}
	if e := ruleAction(t, toggle); e != nil {
		t.Fatal(e)
	}
	c, e = buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	b := c["routing"].(map[string]any)["rules"].([]any)
	if b[len(b)-2].(map[string]any)["outboundTag"] != "block" {
		t.Fatal("reenabled rule missing")
	}
	ruleAction(t, toggle)
	ruleAction(t, url.Values{"action": {"rule-delete"}, "id": {"1"}})
	if setting("rule_disabled:1") != "" {
		t.Fatal("deleted rule left state for reused ID")
	}
}
