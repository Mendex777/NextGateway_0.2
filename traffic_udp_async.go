package main

import (
	"fmt"
	"time"
)

// Historical UDP attribution must never block publication of Linux counters.
// One bounded journal job runs at a time, at most once per 30 seconds.
type udpLookupResult struct {
	epoch  string
	events []ConnectionEvent
}
type asyncUDPLookup struct {
	epoch   string
	running <-chan udpLookupResult
	next    time.Time
	events  map[string]ConnectionEvent
	now     func() time.Time
	lookup  udpRouteLookup
}

func (a *asyncUDPLookup) read(pid, boot string, keys []string, cone bool) ([]ConnectionEvent, error) {
	now := a.now()
	epoch := fmt.Sprintf("%s/%s/%t", pid, boot, cone)
	if a.epoch != epoch {
		a.epoch = epoch
		a.events = map[string]ConnectionEvent{}
		a.next = time.Time{}
	}
	if a.running != nil {
		select {
		case result := <-a.running:
			a.running = nil
			if result.epoch == a.epoch {
				for _, event := range result.events {
					key := udpAssociation(trafficTuple(event.Source, event.SourcePort, event.Destination, event.Port, event.Network), cone)
					if event.Time.After(a.events[key].Time) {
						if len(a.events) >= udpRouteLimit {
							a.events = map[string]ConnectionEvent{}
						}
						a.events[key] = event
					}
				}
			}
		default:
		}
	}
	out := []ConnectionEvent{}
	missing := []string{}
	served := map[string]bool{}
	for _, key := range keys {
		if event, ok := a.events[udpAssociation(key, cone)]; ok {
			out = append(out, event)
			served[udpAssociation(key, cone)] = true
		} else {
			missing = append(missing, key)
		}
	}
	// The tracker retains accepted bindings. Consume cached results so a reused
	// source tuple can request a newer journal association rather than an old one.
	for key := range served {
		delete(a.events, key)
	}
	if len(missing) > 0 && a.running == nil && !now.Before(a.next) {
		pending := append([]string(nil), missing...)
		result := make(chan udpLookupResult, 1)
		a.running = result
		a.next = now.Add(30 * time.Second)
		lookup := a.lookup
		go func() {
			events, _ := lookup(pid, boot, pending, cone)
			result <- udpLookupResult{epoch: epoch, events: events}
		}()
	}
	return out, nil
}
