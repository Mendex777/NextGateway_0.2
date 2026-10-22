package main

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Carry the applied name in the tag so edits or deleted rules cannot relabel history.
func connectionRuleTag(r Rule) string {
	return "ngpanel-rule:" + strconv.Itoa(r.ID) + ":" + base64.RawURLEncoding.EncodeToString([]byte(r.Name))
}
func connectionSystemRuleTag(name string) string {
	return "ngpanel-system:" + base64.RawURLEncoding.EncodeToString([]byte(name))
}

// New Xray releases require globally unique routing rule tags. Keep the encoded
// display name and old tags readable, adding a stable suffix only on duplicates.
func uniqueConnectionRuleTags(config map[string]any) {
	rules := config["routing"].(map[string]any)["rules"].([]any)
	reserved, seen := map[string]bool{}, map[string]bool{}
	for _, raw := range rules {
		if tag, ok := raw.(map[string]any)["ruleTag"].(string); ok {
			reserved[tag] = true
		}
	}
	for i, raw := range rules {
		entry := raw.(map[string]any)
		tag, _ := entry["ruleTag"].(string)
		if tag == "" {
			continue
		}
		if seen[tag] {
			candidate := tag + ":ngpanel-" + strconv.Itoa(i+1)
			for reserved[candidate] {
				candidate += "-"
			}
			entry["ruleTag"] = candidate
			reserved[candidate] = true
		}
		seen[tag] = true
	}
}
func connectionRuleLabel(tag string) string {
	encoded := ""
	if strings.HasPrefix(tag, "ngpanel-rule:") {
		parts := strings.SplitN(strings.TrimPrefix(tag, "ngpanel-rule:"), ":", 2)
		if len(parts) != 2 {
			return ""
		}
		id, e := strconv.Atoi(parts[0])
		if e != nil || id < 1 {
			return ""
		}
		encoded = parts[1]
	} else if strings.HasPrefix(tag, "ngpanel-system:") {
		encoded = strings.TrimPrefix(tag, "ngpanel-system:")
	}
	encoded = strings.SplitN(encoded, ":", 2)[0]
	if len(encoded) > 2048 {
		return ""
	}
	raw, e := base64.RawURLEncoding.DecodeString(encoded)
	if e != nil || len(raw) == 0 || !utf8.Valid(raw) {
		return ""
	}
	label := string(raw)
	for _, c := range label {
		if unicode.IsControl(c) {
			return ""
		}
	}
	return label
}
