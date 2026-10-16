package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// v0.3.38's updater preserves its strict release file list and starts only the
// panel. Provision the collector through the fixed root job after that upgrade.
// The HTTP process never writes system units or gains journal permissions.
func ensureConnectionCollector() {
	for attempt := 0; attempt < 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		state, e := exec.CommandContext(ctx, "systemctl", "show", "ngpanel-connections.service", "--property=LoadState", "--value").Output()
		cancel()
		if e == nil {
			if e := queueConnectionCollector(strings.TrimSpace(string(state)), func() error { return enqueue("connections", nil) }); e == nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	log.Print("Could not queue connection collector setup; check ngpanel-control journal")
}
func queueConnectionCollector(loadState string, submit func() error) error {
	if loadState == "loaded" {
		return nil
	}
	if loadState != "not-found" {
		return fmt.Errorf("collector unit state: %s", loadState)
	}
	return submit()
}
