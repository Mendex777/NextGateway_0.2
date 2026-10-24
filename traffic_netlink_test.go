package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"syscall"
	"testing"
	"time"
)

func TestNetlinkTrafficTuple(t *testing.T) {
	key := []byte{192, 168, 1, 83, 0, 0, 0, 0, 203, 0, 113, 2, 0, 0, 0, 0, 17, 0, 0, 0}
	binary.BigEndian.PutUint16(key[4:6], 12345)
	binary.BigEndian.PutUint16(key[12:14], 443)
	got, err := netlinkTrafficKey("flow_upload", key)
	if err != nil || got != "flow:192.168.1.83|12345|203.0.113.2|443|udp" {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{key[:19], append([]byte(nil), key...)} {
		if len(bad) == 20 {
			bad[16] = 99
		}
		if _, err := netlinkTrafficKey("flow_upload", bad); err == nil {
			t.Fatal("bad key accepted")
		}
	}
}
func TestTrafficNetlinkLiveReadOnly(t *testing.T) {
	if os.Getenv("NG_TRAFFIC_NETLINK_LIVE") != "1" {
		t.Skip("read-only live comparison")
	}
	n := readRuntime().AppliedNetwork
	owner, _ := user.Lookup("ngxray")
	config, err := os.ReadFile("/etc/ngpanel/config.json")
	if err != nil {
		t.Fatal(err)
	}
	exits, err := parseTrafficExits(config)
	if err != nil {
		t.Fatal(err)
	}
	_, want, _ := trafficNFT(n, owner.Uid, exits...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := trafficNetlink{}
	defer r.close()
	raw, err := trafficCommand(ctx, "", "nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic")
	if err != nil {
		t.Fatal(err)
	}
	before, handle, comment, err := parseTrafficNFT(raw)
	if err != nil || comment != want {
		t.Fatal(err, comment, want)
	}
	after, gen, err := r.read(ctx, want, "boot", exits, false)
	if err != nil {
		t.Fatal(err)
	}
	if gen != "boot/nft/"+handle {
		t.Fatal("generation changed", gen, handle)
	}
	for key, value := range before {
		if len(key) > 5 && key[:5] == "flow:" {
			continue
		}
		if got, ok := after[key]; !ok || got.Up < value.Up || got.Down < value.Down {
			t.Fatal("counter/key mismatch", key, value, got)
		}
	}
	flows, flowGen, err := r.read(ctx, want, "boot", exits, true)
	if err != nil || flowGen != gen {
		t.Fatal(flowGen, err)
	}
	matched := 0
	for key := range flows {
		if _, ok := before[key]; ok {
			matched++
		}
	}
	if len(flows) > 0 && matched*100/len(flows) < 70 {
		t.Fatal("flow tuple decoding mismatch", matched, len(flows))
	}
	cpu := func() float64 {
		var a, b syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &a)
		syscall.Getrusage(syscall.RUSAGE_CHILDREN, &b)
		return float64(a.Utime.Sec+a.Stime.Sec+b.Utime.Sec+b.Stime.Sec) + float64(a.Utime.Usec+a.Stime.Usec+b.Utime.Usec+b.Stime.Usec)/1e6
	}
	measure := func(name string, count int, fn func() error) {
		start := time.Now()
		used := cpu()
		for i := 0; i < count; i++ {
			if err := fn(); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: %.3f ms wall / %.3f ms CPU per read", name, float64(time.Since(start).Microseconds())/1000/float64(count), (cpu()-used)*1000/float64(count))
	}
	measure("nft JSON full table", 5, func() error {
		raw, e := trafficCommand(context.Background(), "", "nft", "-j", "-a", "list", "table", "inet", "ngpanel_traffic")
		if e == nil {
			_, _, _, e = parseTrafficNFT(raw)
		}
		return e
	})
	measure("Netlink aggregate", 20, func() error { _, _, e := r.read(context.Background(), want, "boot", exits, false); return e })
	measure("Netlink flows", 10, func() error { _, _, e := r.read(context.Background(), want, "boot", exits, true); return e })
	out, _ := json.Marshal(map[string]any{"devices_exits": len(after), "flows": len(flows), "matched": matched})
	fmt.Println(string(out))
}
