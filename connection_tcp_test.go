package main

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestTCPObservedLifecycle(t *testing.T) {
	now := time.Unix(10000, 0)
	e := ConnectionEvent{ID: "log", Time: now, Network: "tcp", Inbound: "tproxy-in", Source: "192.168.1.56", SourcePort: "1234", Destination: "203.0.113.1", Port: "443", process: "boot|1", StartKnown: true, Domain: "example.com", Rule: "Видео"}
	socket := tcpSocket{"boot|1", "10", e.Source, e.SourcePort, e.Destination, e.Port}
	unknown := updateTCPStates([]ConnectionEvent{e}, nil, nil, now)
	if unknown[0].TCPState != "" || unknown[0].ClosedAt != nil {
		t.Fatal("unobserved socket falsely marked closed")
	}
	events := updateTCPStates([]ConnectionEvent{e}, []tcpSocket{socket}, map[string]bool{}, now)
	if events[0].TCPState != "active" {
		t.Fatal("not active")
	}
	events = retainConnectionEvents(events, now.Add(time.Hour))
	if len(events) != 1 {
		t.Fatal("long lived active connection expired")
	}
	events = updateTCPStates(events, nil, nil, now.Add(time.Hour))
	if events[0].TCPState != "closed" || events[0].ClosedAt == nil || events[0].Domain != "example.com" || events[0].Rule != "Видео" {
		t.Fatal("lost closure or metadata")
	}
	if len(retainConnectionEvents(events, now.Add(time.Hour+time.Minute))) != 1 {
		t.Fatal("history retained by start instead of closure")
	}
	if len(retainConnectionEvents(events, now.Add(time.Hour+16*time.Minute))) != 0 {
		t.Fatal("closed history never expired")
	}
	// A new inode using the same addresses cannot resurrect an old closed session.
	socket.inode = "11"
	events = updateTCPStates(events, []tcpSocket{socket}, nil, now.Add(time.Hour+time.Second))
	if len(events) != 2 || events[0].TCPState != "closed" || events[1].StartKnown || events[1].Rule != "" {
		t.Fatal("reused old session metadata")
	}
	// A source port reused by a different destination is a different connection.
	socket.destination = "203.0.113.2"
	socket.inode = "12"
	events = updateTCPStates(events, []tcpSocket{socket}, nil, now.Add(time.Hour+2*time.Second))
	if len(events) != 3 || events[1].TCPState != "closed" || events[2].Destination != "203.0.113.2" {
		t.Fatal("cross destination association")
	}
}
func TestTCPProvisionalRowReplaced(t *testing.T) {
	now := time.Now()
	s := tcpSocket{"p", "10", "192.168.1.56", "1234", "203.0.113.1", "443"}
	events := updateTCPStates(nil, []tcpSocket{s}, nil, now)
	if len(events) != 1 || events[0].StartKnown {
		t.Fatal("missing provisional row")
	}
	real := events[0]
	real.ID = "log"
	real.socketID = ""
	real.StartKnown = true
	real.Domain = "real.example"
	real.Time = now.Add(-time.Millisecond)
	events = append(events, real)
	events = updateTCPStates(events, []tcpSocket{s}, nil, now)
	events = retainConnectionEvents(events, now)
	if len(events) != 1 || events[0].ID != "log" || events[0].Domain != "real.example" {
		t.Fatal("duplicate or wrong metadata")
	}
	// Normal upstream VPN sockets do not appear as user traffic.
	events = updateTCPStates(nil, []tcpSocket{s}, map[string]bool{s.destination: true}, now)
	if len(events) != 0 {
		t.Fatal("included outbound socket")
	}
}
func TestProcTCPParsing(t *testing.T) {
	header := "  sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
	raw := header + "0: 010000CB:01BB 3801A8C0:04D2 01 00000000:00000000 00:00000000 00000000 996 0 42\n"
	sockets, e := parseTCPSockets(raw, "996", "p")
	if e != nil || len(sockets) != 1 {
		t.Fatal(e)
	}
	if sockets[0].source != "192.168.1.56" || sockets[0].destination != "203.0.0.1" || sockets[0].sourcePort != "1234" || sockets[0].port != "443" {
		t.Fatalf("bad tuple: %#v", sockets)
	}
	if rows, _ := parseTCPSockets(raw, "999", "p"); len(rows) != 0 {
		t.Fatal("wrong owner")
	}
	if _, _, e := procTCPEndpoint("0000000000000000FFFF00003801A8C0:04D2"); e != nil {
		t.Fatal(e)
	}
	if ip, _, _ := procTCPEndpoint("0000000000000000FFFF00003801A8C0:04D2"); ip != "192.168.1.56" {
		t.Fatal(ip)
	}
	if _, e := parseTCPSockets("", "996", "p"); e == nil {
		t.Fatal("accepted failed read")
	}
}
func TestTCPRealSocketLifecycle(t *testing.T) {
	if _, e := os.Stat("/proc/net/tcp"); e != nil {
		t.Skip("Linux socket table required")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	client, e := net.Dial("tcp4", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	server, e := listener.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	src, sp, _ := net.SplitHostPort(client.LocalAddr().String())
	dest, port, _ := net.SplitHostPort(server.LocalAddr().String())
	events := []ConnectionEvent{{ID: "real", Time: time.Now(), Network: "tcp", Source: src, SourcePort: sp, Destination: dest, Port: port, process: "test", StartKnown: true}}
	read := func() []tcpSocket {
		b, e := os.ReadFile("/proc/net/tcp")
		if e != nil {
			t.Fatal(e)
		}
		s, e := parseTCPSockets(string(b), strconv.Itoa(os.Getuid()), "test")
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	events = updateTCPStates(events, read(), map[string]bool{"127.0.0.1": true}, time.Now())
	if events[0].TCPState != "active" {
		t.Fatal("real connection not active")
	}
	client.Close()
	server.Close()
	for i := 0; i < 20; i++ {
		events = updateTCPStates(events, read(), map[string]bool{"127.0.0.1": true}, time.Now())
		if events[0].TCPState == "closed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("real connection not closed")
}
