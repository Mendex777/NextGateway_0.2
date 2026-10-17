package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHomeUsesAppliedRouting(t *testing.T) {
	config := map[string]any{"routing": map[string]any{"rules": []map[string]string{
		{"ruleTag": connectionSystemRuleTag("Служебное правило"), "outboundTag": "api"},
		{"ruleTag": connectionRuleTag(Rule{ID: 1, Name: "Видео"}), "balancerTag": "group-7"},
		{"ruleTag": connectionSystemRuleTag("Маршрут по умолчанию"), "outboundTag": "direct"},
	}}}
	raw, _ := json.Marshal(config)
	got := summarizeHome(raw, Runtime{Groups: []BalanceGroup{{ID: "7", Name: "Применённая группа"}}}, nil)
	if !got.Available || got.Default != "Direct — провайдер" || got.RuleCount != 1 || len(got.Exits) != 2 || got.Exits[0].Name != "Применённая группа" || got.Exits[0].GroupID != "7" {
		t.Fatalf("incorrect summary: %+v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "api") {
		t.Fatal("internal routing exposed")
	}
	if summarizeHome([]byte(`{}`), Runtime{}, nil).Available || summarizeHome([]byte(`invalid`), Runtime{}, nil).Available {
		t.Fatal("missing config presented as applied")
	}
}
