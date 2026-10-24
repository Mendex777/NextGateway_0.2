package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestTrafficDetailFreshness(t *testing.T) {
	now := time.Now()
	sample := trafficDetailResult{at: now.Add(-3 * time.Second), generation: "boot/nft/7", pid: "123"}
	if !sample.valid(now, "boot/nft/7", "123") {
		t.Fatal("normal three-second sample rejected")
	}
	if sample.valid(now, "boot/nft/8", "123") || sample.valid(now, "boot/nft/7", "124") || sample.valid(now.Add(4*time.Second), "boot/nft/7", "123") || sample.valid(now.Add(-4*time.Second), "boot/nft/7", "123") {
		t.Fatal("stale/different process or table accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runTrafficDetails(ctx, make(chan trafficDetailJob), make(chan trafficDetailResult, 1))
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle worker did not stop")
	}
}

// Run only inside a disposable network namespace. This test creates its own
// nft table; it cannot touch the host gateway table or reset host counters.
func TestTrafficNetlinkNamespace(t *testing.T) {
	if os.Getenv("NG_TRAFFIC_NETLINK_NAMESPACE") != "1" {
		t.Skip("requires disposable network namespace")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if self == "" || host == "" || self == host {
		t.Fatal("refusing nft fixture in host namespace")
	}
	const fixture = `table inet ngpanel_traffic {
 comment "test-accounting"
 set upload { type ipv4_addr; counter; elements = { 192.0.2.2 counter packets 3 bytes 300 } }
 set download { type ipv4_addr; counter; elements = { 192.0.2.2 counter packets 5 bytes 700 } }
 set flow_upload { type ipv4_addr . inet_service . ipv4_addr . inet_service . inet_proto; counter; elements = { 192.0.2.2 . 12345 . 203.0.113.2 . 443 . udp counter packets 3 bytes 300 } }
 set flow_download { type ipv4_addr . inet_service . ipv4_addr . inet_service . inet_proto; counter; elements = { 192.0.2.2 . 12345 . 203.0.113.2 . 443 . udp counter packets 5 bytes 700 } }
 counter u_1029a { comment "upload:direct" packets 3 bytes 300 }
 counter d_1029a { comment "download:direct" packets 5 bytes 700 }
}
`
	ctx := context.Background()
	r := trafficNetlink{}
	defer r.close()
	nft := func(text string) {
		t.Helper()
		if _, err := trafficCommand(ctx, text, "nft", "-f", "-"); err != nil {
			t.Fatal(err)
		}
	}
	nft(fixture)
	exits := []trafficExit{{Tag: "direct", Mark: 0x1029a}}
	counts, gen, err := r.read(ctx, "test-accounting", "boot", exits, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]trafficRaw{"192.0.2.2": {Up: 300, Down: 700}, "exit:direct": {Up: 300, Down: 700}}
	if !reflect.DeepEqual(counts, want) {
		t.Fatal(counts)
	}
	flow, fg, err := r.read(ctx, "test-accounting", "boot", exits, true)
	if err != nil || fg != gen || flow["flow:192.0.2.2|12345|203.0.113.2|443|udp"] != want["192.0.2.2"] {
		t.Fatal(flow, fg, err)
	}
	if _, _, err = r.read(ctx, "wrong-schema", "boot", exits, false); err == nil {
		t.Fatal("wrong schema accepted")
	}
	// Persist on a disposable database and simulate a collector restart.
	db, err := sql.Open("sqlite3", t.TempDir()+"/history.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE saved (up INTEGER, down INTEGER, gen TEXT)"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record, _ := advanceTraffic(trafficRecord{}, counts["192.0.2.2"], gen, now)
	if _, err = db.Exec("INSERT INTO saved VALUES(?,?,?)", record.Total.Up, record.Total.Down, gen); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT up,down,gen FROM saved").Scan(&record.Total.Up, &record.Total.Down, &record.Generation); err != nil {
		t.Fatal(err)
	}
	record.At = time.Time{}
	record, value := advanceTraffic(record, counts["192.0.2.2"], gen, now.Add(time.Second))
	if value.UpBytes != 300 || value.DownBytes != 700 {
		t.Fatal("restart double counted", value)
	}
	nft("delete table inet ngpanel_traffic\n" + fixture)
	counts, newGen, err := r.read(ctx, "test-accounting", "boot", exits, false)
	if err != nil || newGen == gen {
		t.Fatal("table generation not detected", err)
	}
	_, value = advanceTraffic(record, counts["192.0.2.2"], newGen, now.Add(2*time.Second))
	if value.UpBytes != 600 || value.DownBytes != 1400 || value.UpBPS != 0 {
		t.Fatal("new table counters lost", value)
	}
	// CLI and direct reader must expose exactly the same keys/generation.
	data, err := exec.Command("nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic").Output()
	if err != nil {
		t.Fatal(err)
	}
	cli, handle, _, err := parseTrafficNFT(data)
	if err != nil || newGen != "boot/nft/"+handle {
		t.Fatal(err)
	}
	for key, v := range counts {
		if cli[key] != v {
			t.Fatal("CLI differs", key)
		}
	}
}
