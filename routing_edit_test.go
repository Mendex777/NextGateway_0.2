package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRuleSwitchReturnsSavedState(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO rules VALUES(1,100,'example','domain','example.com','direct')")
	for _, enabled := range []bool{false, true} {
		values := url.Values{"action": {"rule-toggle"}, "id": {"1"}, "tab": {"routing"}}
		r := httptest.NewRequest("POST", "/action", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		handler(w, r)
		var result struct {
			OK      bool `json:"ok"`
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || !result.OK || result.Enabled != enabled {
			t.Fatalf("unexpected switch response: %d %s", w.Code, w.Body.String())
		}
		if allRules()[0].Disabled == enabled {
			t.Fatal("response does not match saved rule")
		}
	}
}

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
