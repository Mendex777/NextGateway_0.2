package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTCPProbeStopsBeforeStartingXray(t *testing.T) {
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	raw := "vless://11111111-1111-4111-8111-111111111111@" + server.Addr().String() + "?security=none"
	result := probeNodeMode(context.Background(), raw, "tcp")
	if result.State != "ok" || result.Mode != "tcp" || result.HTTPSMS != result.TCPMS {
		t.Fatalf("unexpected TCP result: %+v", result)
	}
	if _, err = probeMode([]string{"invalid"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestProbeDoesNotTreatTCPAsWorkingVPN(t *testing.T) {
	if _, e := os.Stat("/usr/local/bin/xray"); e != nil {
		t.Skip("requires installed Xray")
	}
	state := t.TempDir()
	t.Setenv("NG_STATE", state)
	os.Mkdir(filepath.Join(state, "db"), 0o700)
	server, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	go func() {
		for {
			c, e := server.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	result := probeNode("vless://11111111-1111-4111-8111-111111111111@" + server.Addr().String() + "?security=none")
	if result.State == "ok" || !strings.Contains(result.Message, "HTTPS через VPN не прошёл") {
		t.Fatalf("TCP-only server counted as VPN: %+v", result)
	}
	entries, _ := os.ReadDir(filepath.Join(state, "db"))
	if len(entries) != 0 {
		t.Fatal("probe credentials not cleaned up")
	}
}
