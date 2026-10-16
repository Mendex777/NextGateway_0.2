package main

import (
	"testing"
	"time"
)

func domainAuditEvent(t *testing.T, id, process, source, target string, at time.Time) ConnectionEvent {
	t.Helper()
	e, ok := parseConnection("from "+source+" accepted tcp:"+target+" [tproxy-in -> direct]", id, at)
	if !ok {
		t.Fatal("bad fixture")
	}
	e.process = process
	return e
}
func domainAuditSession(r *domainResolver, id, process, source, domain string, at time.Time) {
	r.observe("[Info] ["+id+"] proxy/dokodemo: received request for "+source, process, at)
	r.observe("[Info] ["+id+"] app/dispatcher: sniffed domain: "+domain, process, at)
	r.observe("[Info] ["+id+"] app/dispatcher: taking detour [direct] for [tcp:"+domain+":443]", process, at)
}
func TestConnectionDomainLateInfoAndIsolation(t *testing.T) {
	at := time.Unix(100, 0)
	r := domainResolver{}
	events := []ConnectionEvent{domainAuditEvent(t, "a", "boot|10", "192.168.1.56:1234", "203.0.113.1:443", at), domainAuditEvent(t, "b", "boot|11", "192.168.1.56:1234", "203.0.113.1:443", at)}
	domainAuditSession(&r, "12", "boot|10", "192.168.1.56:1234", "audit.example", at.Add(time.Millisecond))
	r.enrich(events, at.Add(500*time.Millisecond))
	if events[0].Domain != "audit.example" || events[1].Domain != "" || events[0].Destination != "203.0.113.1" {
		t.Fatalf("bad correlation: %#v", events)
	}
	r.enrich(events, at.Add(10*time.Second))
	if events[0].Domain != "audit.example" || len(r.sessions) != 0 {
		t.Fatal("history lost domain or session did not expire")
	}
}
func TestConnectionDomainAmbiguity(t *testing.T) {
	at := time.Unix(100, 0)
	r := domainResolver{}
	events := []ConnectionEvent{domainAuditEvent(t, "a", "p", "192.168.1.56:1234", "203.0.113.1:443", at), domainAuditEvent(t, "b", "p", "192.168.1.56:1234", "203.0.113.2:443", at)}
	domainAuditSession(&r, "12", "p", "192.168.1.56:1234", "one.example", at)
	r.enrich(events, at)
	if events[0].Domain != "" || events[1].Domain != "" {
		t.Fatal("guessed domain for ambiguous access records")
	}
	events = events[:1]
	domainAuditSession(&r, "13", "p", "192.168.1.56:1234", "two.example", at)
	r.enrich(events, at)
	if events[0].Domain != "" {
		t.Fatal("guessed domain for ambiguous sessions")
	}
}
func TestConnectionDomainNoCrossEndpointOrSessionReuse(t *testing.T) {
	at := time.Unix(100, 0)
	r := domainResolver{}
	event := domainAuditEvent(t, "a", "p", "192.168.1.56:1234", "203.0.113.1:443", at)
	domainAuditSession(&r, "12", "p", "192.168.1.56:4567", "other.example", at)
	events := []ConnectionEvent{event}
	r.enrich(events, at)
	if events[0].Domain != "" {
		t.Fatal("used another source port")
	}
	r.observe("[Info] [12] proxy/dokodemo: received request for 192.168.1.56:1234", "p", at)
	r.observe("[Info] [12] app/dispatcher: taking detour [direct] for [tcp:203.0.113.1:443]", "p", at)
	r.enrich(events, at)
	if events[0].Domain != "" {
		t.Fatal("reused stale domain from a recycled session ID")
	}
}
