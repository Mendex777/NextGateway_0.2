package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Host counters are sampled once per second, shared by all browser sessions.
type dashboardMetrics struct {
	Time        int64   `json:"time"`
	CPU         float64 `json:"cpu"`
	Cores       int     `json:"cores"`
	Memory      uint64  `json:"memory"`
	MemoryTotal uint64  `json:"memoryTotal"`
	Swap        uint64  `json:"swap"`
	SwapTotal   uint64  `json:"swapTotal"`
	Disk        uint64  `json:"disk"`
	DiskTotal   uint64  `json:"diskTotal"`
	Upload      float64 `json:"upload"`
	Download    float64 `json:"download"`
	Sent        uint64  `json:"sent"`
	Received    uint64  `json:"received"`
	TCP         int     `json:"tcp"`
	UDP         int     `json:"udp"`
	Uptime      float64 `json:"uptime"`
	XrayUptime  float64 `json:"xrayUptime"`
	PanelMemory uint64  `json:"panelMemory"`
	Threads     int     `json:"threads"`
	XrayPID     int     `json:"xrayPID"`
	XrayMemory  uint64  `json:"xrayMemory"`
	XrayThreads int     `json:"xrayThreads"`
	Interface   string  `json:"interface"`
}

var dashboardCache struct {
	sync.Mutex
	at          time.Time
	total, idle uint64
	sample      dashboardMetrics
}

func procNumbers(path string) map[string]uint64 {
	result := map[string]uint64{}
	raw, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, e := strconv.ParseUint(f[1], 10, 64)
		if e != nil {
			continue
		}
		if len(f) > 2 && f[2] == "kB" {
			v *= 1024
		}
		result[strings.TrimSuffix(f[0], ":")] = v
	}
	return result
}
func socketCount(path string) int {
	raw, e := os.ReadFile(path)
	if e != nil {
		return 0
	}
	n := len(strings.FieldsFunc(strings.TrimSpace(string(raw)), func(r rune) bool { return r == '\n' })) - 1
	if n < 0 {
		return 0
	}
	return n
}
func hostDashboard() dashboardMetrics {
	dashboardCache.Lock()
	defer dashboardCache.Unlock()
	now := time.Now()
	if now.Sub(dashboardCache.at) < time.Second {
		return dashboardCache.sample
	}
	m := dashboardMetrics{Time: now.UnixMilli(), Cores: runtime.NumCPU()}
	raw, _ := os.ReadFile("/proc/stat")
	lines := strings.Split(string(raw), "\n")
	var total, idle uint64
	if len(lines) > 0 {
		f := strings.Fields(lines[0])
		for i := 1; i < len(f) && i <= 8; i++ {
			v, _ := strconv.ParseUint(f[i], 10, 64)
			total += v
			if i == 4 || i == 5 {
				idle += v
			}
		}
	}
	if total > dashboardCache.total && dashboardCache.total > 0 && idle >= dashboardCache.idle {
		m.CPU = 100 * (1 - float64(idle-dashboardCache.idle)/float64(total-dashboardCache.total))
	}
	mem := procNumbers("/proc/meminfo")
	m.MemoryTotal = mem["MemTotal"]
	if m.MemoryTotal >= mem["MemAvailable"] {
		m.Memory = m.MemoryTotal - mem["MemAvailable"]
	}
	m.SwapTotal = mem["SwapTotal"]
	if m.SwapTotal >= mem["SwapFree"] {
		m.Swap = m.SwapTotal - mem["SwapFree"]
	}
	var disk syscall.Statfs_t
	if syscall.Statfs("/", &disk) == nil {
		m.DiskTotal = disk.Blocks * uint64(disk.Bsize)
		m.Disk = (disk.Blocks - disk.Bfree) * uint64(disk.Bsize)
	}
	n, _ := detectNetwork()
	m.Interface = n.Interface
	if m.Interface != "" {
		read := func(name string) uint64 {
			b, _ := os.ReadFile(filepath.Join("/sys/class/net", m.Interface, "statistics", name))
			v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
			return v
		}
		m.Sent = read("tx_bytes")
		m.Received = read("rx_bytes")
	}
	prev := dashboardCache.sample
	seconds := now.Sub(dashboardCache.at).Seconds()
	if !dashboardCache.at.IsZero() && m.Interface == prev.Interface && seconds > 0 {
		if m.Sent >= prev.Sent {
			m.Upload = float64(m.Sent-prev.Sent) / seconds
		}
		if m.Received >= prev.Received {
			m.Download = float64(m.Received-prev.Received) / seconds
		}
	}
	m.TCP = socketCount("/proc/net/tcp") + socketCount("/proc/net/tcp6")
	m.UDP = socketCount("/proc/net/udp") + socketCount("/proc/net/udp6")
	up, _ := os.ReadFile("/proc/uptime")
	fields := strings.Fields(string(up))
	if len(fields) > 0 {
		m.Uptime, _ = strconv.ParseFloat(fields[0], 64)
	}
	proc := procNumbers("/proc/self/status")
	m.PanelMemory = proc["VmRSS"]
	m.Threads = int(proc["Threads"])
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "systemctl", "show", "xray", "--property=ActiveEnterTimestampMonotonic,MainPID").Output()
	if e == nil {
		properties := map[string]string{}
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				properties[key] = value
			}
		}
		micros, _ := strconv.ParseFloat(properties["ActiveEnterTimestampMonotonic"], 64)
		if micros > 0 && m.Uptime > micros/1e6 {
			m.XrayUptime = m.Uptime - micros/1e6
		}
		m.XrayPID, _ = strconv.Atoi(properties["MainPID"])
		if m.XrayPID > 0 {
			process := procNumbers(filepath.Join("/proc", strconv.Itoa(m.XrayPID), "status"))
			m.XrayMemory = process["VmRSS"]
			m.XrayThreads = int(process["Threads"])
		}
	}
	dashboardCache.at = now
	dashboardCache.total = total
	dashboardCache.idle = idle
	dashboardCache.sample = m
	return m
}
func dashboardConfig() (json.RawMessage, error) {
	raw, e := os.ReadFile(filepath.Join(stateDir(), "applied-config.json"))
	if e != nil {
		return nil, e
	}
	var value any
	if e = json.Unmarshal(raw, &value); e != nil {
		return nil, e
	}
	return json.RawMessage(raw), nil
}
