package main

import (
	"strings"
	"testing"
)

func TestReadinessDoesNotTreatAppliedGatewayAsRunning(t *testing.T) {
	p := Page{Runtime: Runtime{Gateway: true}, Service: "inactive"}
	if strings.Contains(overallReadiness(p), "работает") {
		t.Fatal("stopped core reported healthy")
	}
	p.Service = "active"
	p.GatewayHealth = "Missing TPROXY route"
	if strings.Contains(overallReadiness(p), "работает") {
		t.Fatal("missing route reported healthy")
	}
	p.GatewayHealth = ""
	p.Pending = true
	if !strings.Contains(overallReadiness(p), "неприменённые") {
		t.Fatal("pending settings hidden")
	}
}
func TestCoreUpdateComparison(t *testing.T) {
	for _, sample := range []struct {
		a, b string
		want bool
	}{{"v26.7.28", "26.3.27", true}, {"v26.3.27", "26.3.27", false}, {"v25.12.8", "26.3.27", false}, {"v26.7.28-beta", "26.3.27", false}} {
		if newerCore(sample.a, sample.b) != sample.want {
			t.Fatal(sample)
		}
	}
}
