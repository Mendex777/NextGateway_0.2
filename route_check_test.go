package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestDeviceRulePreservesOtherDevices(t *testing.T) {
	configDatabase(t)
	values := url.Values{"action": {"rule-add"}, "name": {"phone"}, "priority": {"10"}, "kind": {"device"}, "source": {"192.168.1.56"}, "target": {"block"}}
	if e := ruleAction(t, values); e != nil {
		t.Fatal(e)
	}
	rules := allRules()
	if len(rules) != 1 || rules[0].Source != "192.168.1.56" {
		t.Fatal("source not saved")
	}
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	routes := c["routing"].(map[string]any)["rules"].([]any)
	r := routes[len(routes)-2].(map[string]any)
	if r["source"].([]string)[0] != "192.168.1.56" || r["domain"] != nil || r["ip"] != nil {
		t.Fatal("device rule generated incorrectly")
	}
	if c := checkRoute("example.com", "1.1.1.1", "192.168.1.56", rules, "direct"); c.Target != "block" {
		t.Fatal(c)
	}
	if c := checkRoute("example.com", "1.1.1.1", "192.168.1.57", rules, "direct"); c.Target != "direct" {
		t.Fatal(c)
	}
	if c := checkRoute("example.com", "", "", rules, "direct"); c.Target != "direct" || !strings.Contains(c.Warning, "IP устройства") || !strings.Contains(c.Result, "Предварительно") {
		t.Fatal("missing source must be marked uncertain")
	}
	if c := checkRoute("", "192.168.1.1", "192.168.1.56", rules, "proxy"); c.Target != "direct" {
		t.Fatal("local exception lost")
	}
	values.Set("source", "::1")
	if e := ruleAction(t, values); e == nil {
		t.Fatal("IPv6 source accepted")
	}
	if len(allRules()) != 1 {
		t.Fatal("invalid rule inserted")
	}
	values.Set("action", "rule-update")
	values.Set("id", "1")
	values.Set("source", "192.168.1.56/24")
	values.Set("kind", "domain")
	values.Set("value", "example.com")
	if e := ruleAction(t, values); e != nil {
		t.Fatal(e)
	}
	rules = allRules()
	if rules[0].Source != "192.168.1.0/24" {
		t.Fatal("CIDR not normalized")
	}
	if c := checkRoute("notexample.com", "1.1.1.1", "192.168.1.57", rules, "direct"); c.Target != "direct" {
		t.Fatal("AND conditions not enforced")
	}
	ruleAction(t, url.Values{"action": {"rule-delete"}, "id": {"1"}})
	if setting("rule_source:1") != "" {
		t.Fatal("source survived deletion")
	}
}
func TestRouteCheckerPriorityAndMissingAddress(t *testing.T) {
	if _, e := validateSource("::ffff:192.168.1.56/128"); e == nil {
		t.Fatal("IPv4-mapped IPv6 source accepted")
	}
	rules := []Rule{{ID: 1, Priority: 1, Kind: "ip", Value: "8.8.8.0/24", Target: "block"}, {ID: 2, Priority: 10, Kind: "domain", Value: "domain:example.com", Target: "proxy"}}
	if c := checkRoute("www.example.com", "8.8.8.8", "", rules, "direct"); c.Target != "block" {
		t.Fatal(c)
	}
	if c := checkRoute("www.example.com", "1.1.1.1", "", rules, "direct"); c.Target != "proxy" {
		t.Fatal(c)
	}
	if c := checkRoute("www.example.com", "", "", rules, "direct"); c.Target != "proxy" || !strings.Contains(c.Warning, "IPv4 назначения") || !strings.Contains(c.Result, "Предварительно") {
		t.Fatal("unknown IP rule must leave an explicit uncertainty warning")
	}
	rules[0].Disabled = true
	if c := checkRoute("www.example.com", "", "", rules, "direct"); c.Target != "proxy" {
		t.Fatal(c)
	}
	for _, value := range []string{"https://example.com", "example.com:443", "example.com/path"} {
		if c := checkRoute(value, "", "", rules, "direct"); c.Target != "" {
			t.Fatal("invalid domain accepted")
		}
	}
	if domainMatches("domain:example.com", "badexample.com") || domainMatches("full:example.com", "www.example.com") || !domainMatches("regexp:^www\\.", "www.example.com") {
		t.Fatal("domain semantics wrong")
	}
}
