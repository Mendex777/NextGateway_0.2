package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHysteriaImportAndConfiguration(t *testing.T) {
	raw := "hy2://user%3Apassword@example.com?obfs=salamander&obfs-password=test-obfs&sni=example.com#test"
	nodes, e := parseNodes(raw)
	if e != nil || len(nodes) != 1 {
		t.Fatal("Hysteria import failed", e)
	}
	out, e := nodeOutbound(raw)
	if e != nil {
		t.Fatal(e)
	}
	settings := out["settings"].(map[string]any)
	stream := out["streamSettings"].(map[string]any)
	if settings["port"] != 443 || stream["hysteriaSettings"].(map[string]any)["auth"] != "user:password" {
		t.Fatal("password or default port lost")
	}
	if stream["tlsSettings"].(map[string]any)["allowInsecure"] != false {
		t.Fatal("TLS verification disabled by default")
	}
	if _, e = os.Stat("/usr/local/bin/xray"); e == nil {
		state := t.TempDir()
		t.Setenv("NG_STATE", state)
		os.Mkdir(filepath.Join(state, "db"), 0700)
		if e = validateXrayOutbound(out); e != nil {
			t.Fatal(e)
		}
	}
	for _, bad := range []string{"hy2://@example.com", "hy2://p@[::1]:443", "hy2://p@example.com:65536", "hy2://p@example.com?obfs=unknown", "hy2://p@example.com?obfs=salamander", "hy2://p@example.com?fm=%7B%22sockopt%22%3A%7B%7D%7D"} {
		if _, e = nodeOutbound(bad); e == nil {
			t.Fatal("invalid Hysteria accepted")
		}
	}
}
