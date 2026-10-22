package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratedRoutingTagsAreUniqueAndKeepDisplayNames(t *testing.T) {
	configDatabase(t)
	config, err := buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	dns := 0
	for _, raw := range config["routing"].(map[string]any)["rules"].([]any) {
		entry := raw.(map[string]any)
		tag, _ := entry["ruleTag"].(string)
		if tag == "" || seen[tag] {
			t.Fatal("duplicate or missing generated routing tag")
		}
		seen[tag] = true
		if connectionRuleLabel(tag) == "Политика DNS" {
			dns++
		}
	}
	if dns < 3 {
		t.Fatal("DNS labels lost")
	}
	before, _ := json.Marshal(config)
	uniqueConnectionRuleTags(config)
	after, _ := json.Marshal(config)
	if string(before) != string(after) {
		t.Fatal("tag normalization is not idempotent")
	}
}
func TestUniqueTagsPreserveRuleContentsAndLegacyLabels(t *testing.T) {
	tag := connectionSystemRuleTag("Политика DNS")
	config := map[string]any{"routing": map[string]any{"rules": []any{
		map[string]any{"ruleTag": tag, "inboundTag": []string{"dns-in"}, "outboundTag": "dns-out"},
		map[string]any{"ruleTag": tag, "inboundTag": []string{"dns-upstream"}, "outboundTag": "direct"},
		map[string]any{"ruleTag": tag + ":ngpanel-2", "outboundTag": "block"},
	}}}
	uniqueConnectionRuleTags(config)
	rules := config["routing"].(map[string]any)["rules"].([]any)
	second := rules[1].(map[string]any)
	if second["outboundTag"] != "direct" || second["inboundTag"].([]string)[0] != "dns-upstream" || second["ruleTag"] == rules[2].(map[string]any)["ruleTag"] {
		t.Fatal("rule content changed or suffix collided")
	}
	if connectionRuleLabel(second["ruleTag"].(string)) != "Политика DNS" || connectionRuleLabel(tag) != "Политика DNS" {
		t.Fatal("system label not preserved")
	}
	user := connectionRuleTag(Rule{ID: 9, Name: "Google"})
	if !strings.HasPrefix(user, "ngpanel-rule:") || connectionRuleLabel(user+":ngpanel-9") != "Google" {
		t.Fatal("user label not preserved")
	}
}
