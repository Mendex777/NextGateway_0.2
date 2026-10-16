package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type tcpSocket struct{ process, inode, source, sourcePort, destination, port string }

func (s tcpSocket) key() string { return s.process + "|" + s.inode }
func tcpTuple(process, source, sourcePort, destination, port string) string {
	return process + "|" + net.JoinHostPort(source, sourcePort) + "|" + net.JoinHostPort(destination, port)
}
func (s tcpSocket) tuple() string {
	return tcpTuple(s.process, s.source, s.sourcePort, s.destination, s.port)
}

// Linux prints IPv4 and each IPv6 word in host byte order (our supported Linux hosts are little endian).
func procTCPEndpoint(value string) (string, string, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid socket endpoint")
	}
	b, e := hex.DecodeString(parts[0])
	if e != nil || (len(b) != 4 && len(b) != 16) {
		return "", "", fmt.Errorf("invalid socket address")
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+3] = b[i+3], b[i]
		b[i+1], b[i+2] = b[i+2], b[i+1]
	}
	p, e := strconv.ParseUint(parts[1], 16, 16)
	if e != nil {
		return "", "", e
	}
	return net.IP(b).String(), strconv.FormatUint(p, 10), nil
}
func parseTCPSockets(raw, uid, process string) ([]tcpSocket, error) {
	if !strings.Contains(raw, "local_address") {
		return nil, fmt.Errorf("missing TCP socket table header")
	}
	out := []tcpSocket{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 10 {
			return nil, fmt.Errorf("incomplete TCP socket table")
		}
		if f[7] != uid || f[3] == "0A" || f[3] == "06" || f[3] == "07" || f[9] == "0" {
			continue
		}
		dest, port, e := procTCPEndpoint(f[1])
		if e != nil {
			return nil, e
		}
		src, sp, e := procTCPEndpoint(f[2])
		if e != nil {
			return nil, e
		}
		out = append(out, tcpSocket{process, f[9], src, sp, dest, port})
	}
	return out, nil
}
func readXrayTCP(uid string) ([]tcpSocket, map[string]bool, error) {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return nil, nil, e
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return nil, nil, e
	}
	processes := []string{}
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		dir := "/proc/" + entry.Name()
		name, _ := os.ReadFile(dir + "/comm")
		if strings.TrimSpace(string(name)) != "xray" {
			continue
		}
		status, e := os.ReadFile(dir + "/status")
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				f := strings.Fields(line)
				if len(f) > 1 && f[1] == uid {
					processes = append(processes, strings.ReplaceAll(strings.TrimSpace(string(boot)), "-", "")+"|"+entry.Name())
				}
			}
		}
	}
	if len(processes) == 0 {
		return []tcpSocket{}, map[string]bool{}, nil
	}
	if len(processes) != 1 {
		return nil, nil, fmt.Errorf("multiple Xray processes under service user")
	}
	addresses, e := net.InterfaceAddrs()
	if e != nil {
		return nil, nil, e
	}
	local := map[string]bool{}
	for _, addr := range addresses {
		ip, _, e := net.ParseCIDR(addr.String())
		if e == nil {
			local[ip.String()] = true
		}
	}
	out := []tcpSocket{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, e := os.ReadFile(path)
		if os.IsNotExist(e) && strings.HasSuffix(path, "6") {
			continue
		}
		if e != nil {
			return nil, nil, e
		}
		rows, e := parseTCPSockets(string(raw), uid, processes[0])
		if e != nil {
			return nil, nil, e
		}
		out = append(out, rows...)
	}
	return out, local, nil
}

func updateTCPStates(events []ConnectionEvent, sockets []tcpSocket, local map[string]bool, now time.Time) []ConnectionEvent {
	byTuple := map[string]int{}
	for i := range events {
		e := &events[i]
		if e.Network == "tcp" && e.TCPState != "closed" {
			byTuple[tcpTuple(e.process, e.Source, e.SourcePort, e.Destination, e.Port)] = i
		}
	}
	seen := map[string]bool{}
	for _, s := range sockets {
		index, found := byTuple[s.tuple()]
		if found && events[index].socketID != "" && events[index].socketID != s.key() {
			events[index].TCPState = "closed"
			t := now
			events[index].ClosedAt = &t
			found = false
		}
		// A TPROXY accepted socket has the original remote destination as its local
		// address. Ordinary upstream VPN sockets are bound to an address of this VM.
		if !found {
			ip := net.ParseIP(s.source)
			if local[s.destination] || ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			events = append(events, ConnectionEvent{ID: "socket:" + s.key(), Time: now, Source: s.source, SourcePort: s.sourcePort, Destination: s.destination, Port: s.port, Network: "tcp", Inbound: "tproxy-in", process: s.process})
			index = len(events) - 1
		}
		event := &events[index]
		event.TCPState = "active"
		event.socketID = s.key()
		event.ClosedAt = nil
		seen[s.key()] = true
		// Replace a provisional socket-only row once its access log arrives.
		if event.StartKnown {
			for j := range events {
				old := &events[j]
				if j != index && !old.StartKnown && old.socketID == s.key() {
					old.superseded = true
				}
			}
		}
	}
	for i := range events {
		e := &events[i]
		if e.TCPState == "active" && !seen[e.socketID] {
			e.TCPState = "closed"
			t := now
			e.ClosedAt = &t
		}
	}
	return events
}

func retainConnectionEvents(events []ConnectionEvent, now time.Time) []ConnectionEvent {
	result := make([]ConnectionEvent, 0, len(events))
	cutoff := now.Add(-15 * time.Minute)
	for _, e := range events {
		at := e.Time
		if e.ClosedAt != nil {
			at = *e.ClosedAt
		}
		if !e.superseded && (e.TCPState == "active" || !at.Before(cutoff)) {
			result = append(result, e)
		}
	}
	// Keep active sockets ahead of bounded history, including connections older than 15 minutes.
	if len(result) > connectionLimit {
		remove := len(result) - connectionLimit
		kept := result[:0]
		for _, e := range result {
			if remove > 0 && e.TCPState != "active" {
				remove--
				continue
			}
			kept = append(kept, e)
		}
		result = kept
	}
	if len(result) > connectionLimit {
		result = result[len(result)-connectionLimit:]
	}
	return result
}
