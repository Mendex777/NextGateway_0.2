package main

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestXHTTPEncryptionAndExtra(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	encryption := "mlkem768x25519plus.native.1rtt." + key
	q := url.Values{"type": {"xhttp"}, "encryption": {encryption}, "extra": {`{"scMaxEachPostBytes":"1000000","scMinPostsIntervalMs":"30","xPaddingBytes":"100-1000"}`}, "path": {"/test"}}
	out, e := vlessOutbound("vless://11111111-1111-4111-8111-111111111111@example.com:443?" + q.Encode())
	if e != nil {
		t.Fatal(e)
	}
	user := out["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	if user["encryption"] != encryption {
		t.Fatal("encryption was lost")
	}
	stream := out["streamSettings"].(map[string]any)
	xhttp := stream["xhttpSettings"].(map[string]any)
	if xhttp["extra"].(map[string]any)["xPaddingBytes"] != "100-1000" || xhttp["path"] != "/test" {
		t.Fatal("extra was lost")
	}
	if _, e = os.Stat("/usr/local/bin/xray"); e == nil {
		state := t.TempDir()
		t.Setenv("NG_STATE", state)
		os.Mkdir(filepath.Join(state, "db"), 0700)
		if e = validateXrayOutbound(out); e != nil {
			t.Fatal(e)
		}
	}
	for _, extra := range []string{`[]`, `{"sockopt":{"mark":0}}`, `{"sessionIDLength":"20"}`, `{"downloadSettings":{"sockopt":{"mark":0}}}`} {
		q.Set("extra", extra)
		if _, e = vlessOutbound("vless://id@example.com:443?" + q.Encode()); e == nil {
			t.Fatalf("unsupported extra accepted: %s", extra)
		}
	}
	q.Set("extra", "null")
	if _, e = xhttpSettings(q); e != nil {
		t.Fatal("null extra should mean default")
	}
	if validateEncryption("mlkem768x25519plus.native.1rtt.invalid") == nil {
		t.Fatal("encryption without key accepted")
	}
}

func TestTLSCertificatePinAndGRPCAuthority(t *testing.T) {
	out, e := vlessOutbound("vless://id@example.com:443?type=grpc&security=tls&authority=grpc.example.com&pcs=testpin")
	if e != nil {
		t.Fatal(e)
	}
	stream := out["streamSettings"].(map[string]any)
	if stream["tlsSettings"].(map[string]any)["pinnedPeerCertSha256"] != "testpin" || stream["grpcSettings"].(map[string]any)["authority"] != "grpc.example.com" {
		t.Fatal("TLS pin or authority lost")
	}
}

// Opt-in read-only audit against imported subscriptions; no connections are made.
func TestImportedCompatibility(t *testing.T) {
	path := os.Getenv("NG_AUDIT_DB")
	if path == "" {
		t.Skip("requires NG_AUDIT_DB")
	}
	c, e := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	state := t.TempDir()
	t.Setenv("NG_STATE", state)
	os.Mkdir(filepath.Join(state, "db"), 0700)
	rows, e := c.Query("SELECT id,uri FROM nodes")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	supported, unsupported, rejected := 0, 0, 0
	for rows.Next() {
		var id int
		var raw string
		rows.Scan(&id, &raw)
		out, e := nodeOutbound(raw)
		if e != nil {
			unsupported++
			t.Logf("node %d: %s", id, e)
			continue
		}
		if e = validateXrayOutbound(out); e != nil {
			rejected++
			t.Logf("node %d: core rejected", id)
		} else {
			supported++
		}
	}
	fmt.Printf("Imported audit: supported=%d unsupported=%d core-rejected=%d\n", supported, unsupported, rejected)
	if rejected > 0 {
		t.Fatal("converter produced configurations rejected by installed Xray")
	}
}

func TestUnsupportedSelectionKeepsCurrentNode(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO nodes VALUES(1,'vless://id@example.com:443?type=unknown')")
	saveSetting("selected_node", "old")
	r := httptest.NewRequest("POST", "/action", nil)
	r.Form = url.Values{"action": {"select-node"}, "id": {"1"}}
	handled, _, err := controlAction(r)
	if !handled || err == nil || setting("selected_node") != "old" {
		t.Fatal("unsupported selection changed active choice")
	}
}
