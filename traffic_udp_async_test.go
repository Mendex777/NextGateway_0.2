package main

import (
	"fmt"
	"testing"
	"time"
)

func TestAsyncUDPDoesNotBlockAndCoalescesJournalJobs(t *testing.T) {
	now := time.Now()
	event, key := udpFixture(now)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	defer close(release)
	a := asyncUDPLookup{now: func() time.Time { return now }, lookup: func(string, string, []string, bool) ([]ConnectionEvent, error) {
		started <- struct{}{}
		<-release
		finished <- struct{}{}
		return []ConnectionEvent{event}, nil
	}}
	returned := make(chan struct{})
	go func() { a.read("123", "boot", []string{key}, false); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Journal blocked accounting")
	}
	<-started
	for i := 0; i < 10; i++ {
		a.read("123", "boot", []string{key}, false)
	}
	select {
	case <-started:
		t.Fatal("Concurrent journal jobs")
	default:
	}
	release <- struct{}{}
	<-finished
	deadline := time.Now().Add(time.Second)
	for {
		got, _ := a.read("123", "boot", []string{key}, false)
		if len(got) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Recovered route lost")
		}
		time.Sleep(time.Millisecond)
	}
	a.read("123", "boot", []string{key + "missing"}, false)
	select {
	case <-started:
		t.Fatal("Journal retry burst")
	default:
	}
}
func TestAsyncUDPRateLimitAfterErrors(t *testing.T) {
	now := time.Now()
	done := make(chan struct{}, 2)
	_, key := udpFixture(now)
	a := asyncUDPLookup{now: func() time.Time { return now }, lookup: func(string, string, []string, bool) ([]ConnectionEvent, error) {
		done <- struct{}{}
		return nil, fmt.Errorf("journal unavailable")
	}}
	a.read("123", "boot", []string{key}, false)
	<-done
	time.Sleep(time.Millisecond)
	now = now.Add(29 * time.Second)
	a.read("123", "boot", []string{key}, false)
	select {
	case <-done:
		t.Fatal("Failed lookup retried too soon")
	default:
	}
	now = now.Add(time.Second)
	a.read("123", "boot", []string{key}, false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Retry missing")
	}
}
func TestAsyncUDPRejectsOldProcessResults(t *testing.T) {
	now := time.Now()
	event, key := udpFixture(now)
	finished := make(chan struct{}, 2)
	a := asyncUDPLookup{now: func() time.Time { return now }, lookup: func(string, string, []string, bool) ([]ConnectionEvent, error) {
		finished <- struct{}{}
		return []ConnectionEvent{event}, nil
	}}
	a.read("123", "boot", []string{key}, false)
	<-finished
	time.Sleep(time.Millisecond)
	got, _ := a.read("999", "other", []string{key}, false)
	if len(got) > 0 {
		t.Fatal("Old process attribution leaked")
	}
}
