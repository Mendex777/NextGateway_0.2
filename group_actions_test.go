package main

import (
	"testing"
	"time"
)

func TestManualChoiceResumesPolicy(t *testing.T) {
	g, all, state, now := policyFixture()
	state.Manual = true
	state.ManualChecked = all[state.Current].Checked
	state.Switched = time.Time{}
	if next := decideGroup(g, all, state, now); next.Current != state.Current {
		t.Fatal("healthy manual node replaced")
	}
	sample := all[state.Current]
	sample.Alive = false
	sample.Checked++
	all[state.Current] = sample
	if next := decideGroup(g, all, state, now); next.Current == state.Current {
		t.Fatal("failed manual node retained")
	}
	sample.Alive = true
	sample.DelayMS = 2000
	all[state.Current] = sample
	next := decideGroup(g, all, state, now)
	if next.Current != state.Current || next.Bad != 1 {
		t.Fatal("failure count ignored")
	}
	sample.Checked++
	all[state.Current] = sample
	if next = decideGroup(g, all, next, now); next.Current == state.Current {
		t.Fatal("manual choice blocked threshold switch")
	}
}
