package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRulesUseSeparateVPNNodes(t *testing.T) {
	configDatabase(t)
	db.Exec("ALTER TABLE nodes ADD COLUMN name TEXT")
	db.Exec("INSERT INTO nodes VALUES(1,'vless://11111111-1111-4111-8111-111111111111@127.0.0.1:443?security=none','VLESS'),(2,'hy2://fixture@127.0.0.1:444','Hysteria')")
	db.Exec("INSERT INTO rules VALUES(1,1,'first','domain','domain:one.test','node:1'),(2,2,'second','domain','domain:two.test','node:2'),(3,3,'same','domain','domain:three.test','node:1')")
	// Explicit VPN rules must work without a global selected node.
	c, e := buildConfig()
	if e != nil {
		t.Fatal(e)
	}
	out := c["outbounds"].([]any)
	if len(out) != 5 {
		t.Fatal("unused or duplicate outbound", len(out))
	}
	rules := c["routing"].(map[string]any)["rules"].([]any)
	for i, tag := range []string{"node-1", "node-2", "node-1"} {
		if rules[4+i].(map[string]any)["outboundTag"] != tag {
			t.Fatal("incorrect rule outbound")
		}
	}
	if _, e = os.Stat("/usr/local/bin/xray"); e == nil {
		b, _ := json.Marshal(c)
		file := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(file, b, 0600)
		if exec.Command("/usr/local/bin/xray", "run", "-test", "-c", file).Run() != nil {
			t.Fatal("multi outbound config rejected")
		}
	}
	r := httptest.NewRequest("POST", "/action", nil)
	r.Form = url.Values{"action": {"node-delete"}, "id": {"1"}}
	if _, _, e = controlAction(r); e != nil {
		t.Fatal(e)
	}
	db.Exec("DELETE FROM nodes WHERE id=2")
	if _, e = buildConfig(); e != nil {
		t.Fatal(e)
	}
	db.Exec("DELETE FROM rules WHERE id=2")
	db.Exec("INSERT INTO settings VALUES('rule_disabled:2','1')")
	saveSetting("selected_node", "2")
	if _, e = buildConfig(); e != nil {
		t.Fatal("explicit rules incorrectly depend on global selection", e)
	}
}
