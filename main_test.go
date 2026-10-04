package main

import (
	"encoding/base64"
	"testing"
)

func TestSubscriptionFormats(t *testing.T) {
	raw := "vless://example-id@example.com:443?type=tcp&security=reality#Test"
	for _, s := range []string{raw, base64.StdEncoding.EncodeToString([]byte(raw)), base64.RawURLEncoding.EncodeToString([]byte(raw))} {
		n, e := parseNodes(s)
		if e != nil || len(n) != 1 {
			t.Fatalf("parse: %v", e)
		}
	}
}
func TestRejectEmptyAndMalformed(t *testing.T) {
	for _, s := range []string{"<html>Error</html>", "vless://example.com:443", "vless://id@example.com"} {
		if _, e := parseNodes(s); e == nil {
			t.Fatal("invalid subscription accepted")
		}
	}
}
