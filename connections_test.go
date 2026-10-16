package main

import (
	"testing"
	"time"
)

func TestConnectionParser(t *testing.T) {
	for _, test := range []struct{ message, source, target, network, out string }{
		{"2026/10/16 12:00:00 from 192.168.1.56:12345 accepted tcp:example.com:443 [tproxy-in -> node-12]", "192.168.1.56", "example.com", "tcp", "node-12"},
		{"from tcp:[2001:db8::1]:123 accepted tcp:[2001:db8::2]:443 [test-socks >> direct]", "2001:db8::1", "2001:db8::2", "tcp", "direct"},
		{"from 192.168.1.56:456 accepted udp:1.1.1.1:53 [dns-in -> dns-out]", "192.168.1.56", "1.1.1.1", "udp", "dns-out"},
	} {
		event, ok := parseConnection(test.message, "cursor", time.Unix(123, 0))
		if !ok || event.Source != test.source || event.Destination != test.target || event.Network != test.network || event.Outbound != test.out || event.ID != "cursor" {
			t.Fatalf("bad event: %#v %v", event, ok)
		}
	}
	for _, message := range []string{"some warning with a private token", "from bad:123 accepted tcp:example.com:443 [tproxy-in -> direct]", "from 192.168.1.56:123 rejected tcp:example.com:443 [tproxy-in -> direct]", "from 192.168.1.56:123 accepted tcp:example.com:443 [ambiguous]"} {
		if _, ok := parseConnection(message, "", time.Time{}); ok {
			t.Fatalf("accepted invalid record %q", message)
		}
	}
}
