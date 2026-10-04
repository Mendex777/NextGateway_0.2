package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Device struct {
	IP, MAC, Name, AutoName, Vendor, Seen, Target, TargetLabel string
	Manual                                                     bool
	RuleID                                                     int64
}

var deviceLock sync.Mutex

func deviceIP(ip string) bool { return validDeviceIP(ip, gatewayNetwork()) }
func validDeviceIP(ip string, n GatewayNetwork) bool {
	v := net.ParseIP(ip)
	_, subnet, e := net.ParseCIDR(n.CIDR)
	if e != nil || v == nil || v.To4() == nil || !subnet.Contains(v) || v.String() != ip || ip == n.Address || ip == n.Router || v.Equal(subnet.IP) {
		return false
	}
	broadcast := append(net.IP(nil), subnet.IP.To4()...)
	for i := range broadcast {
		broadcast[i] |= ^subnet.Mask[i]
	}
	return !v.Equal(broadcast)
}
func devices() []Device {
	n := gatewayNetwork()
	rows, e := db.Query("SELECT value FROM settings WHERE key LIKE 'device:%'")
	if e != nil {
		return nil
	}
	var out []Device
	for rows.Next() {
		var raw string
		var d Device
		if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &d) == nil && validDeviceIP(d.IP, n) {
			out = append(out, d)
		}
	}
	rows.Close()
	for i := range out {
		d := &out[i]
		if !d.Manual {
			d.Name = d.AutoName
			if d.Name == "" {
				if d.Vendor != "" && d.Vendor != "Случайный / локальный MAC" {
					d.Name = d.Vendor
				}
			}
			if d.Name == "" {
				d.Name = d.IP
			}
		}
		if d.RuleID != 0 {
			var target, disabled string
			if db.QueryRow("SELECT target,COALESCE((SELECT value FROM settings WHERE key='rule_disabled:' || rules.id),'0') FROM rules WHERE id=?", d.RuleID).Scan(&target, &disabled) == nil {
				d.Target = target
				if disabled == "1" {
					d.TargetLabel = " (отключено)"
				}
			} else {
				d.RuleID = 0
				d.Target = ""
			}
		}
		suffix := d.TargetLabel
		d.TargetLabel = "Общие правила"
		if d.Target != "" {
			d.TargetLabel = targetLabel(d.Target) + suffix
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytesIP(out[i].IP) < bytesIP(out[j].IP) })
	return out
}
func bytesIP(ip string) uint32 {
	b := net.ParseIP(ip).To4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func discoverDevices() error {
	if !deviceLock.TryLock() {
		return fmt.Errorf("Обнаружение уже выполняется")
	}
	saveSetting("device_discovery", "Обнаружение выполняется…")
	go func() {
		defer deviceLock.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		raw, e := exec.CommandContext(ctx, "ip", "-j", "-4", "neigh", "show", "dev", gatewayNetwork().Interface).Output()
		if e != nil {
			saveSetting("device_discovery", "Не удалось прочитать таблицу соседей")
			return
		}
		var entries []struct {
			IP  string `json:"dst"`
			MAC string `json:"lladdr"`
		}
		if json.Unmarshal(raw, &entries) != nil {
			saveSetting("device_discovery", "Некорректный ответ таблицы соседей")
			return
		}
		count := 0
		for _, entry := range entries {
			if !deviceIP(entry.IP) || entry.MAC == "" || ctx.Err() != nil {
				continue
			}
			if _, err := net.ParseMAC(entry.MAC); err != nil {
				continue
			}
			auto := lookupDeviceName(ctx, entry.IP)
			vendor := deviceVendor(entry.MAC)
			deviceLockRecord.Lock()
			var d Device
			json.Unmarshal([]byte(setting("device:"+entry.IP)), &d)
			d.IP = entry.IP
			d.MAC = entry.MAC
			d.Vendor = vendor
			if auto != "" {
				d.AutoName = auto
			}
			d.Seen = time.Now().UTC().Format(time.RFC3339)
			b, _ := json.Marshal(d)
			saveSetting("device:"+entry.IP, string(b))
			deviceLockRecord.Unlock()
			count++
		}
		saveSetting("device_discovery", fmt.Sprintf("Обнаружено устройств: %d. Обновлено: %s МСК", count, time.Now().In(time.FixedZone("MSK", 10800)).Format("02.01.2006 15:04")))
	}()
	return nil
}

var deviceLockRecord sync.Mutex

func lookupDeviceName(parent context.Context, ip string) string {
	ctx, cancel := context.WithTimeout(parent, 1200*time.Millisecond)
	defer cancel()
	resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(gatewayNetwork().Router, "53"))
	}}
	if names, e := resolver.LookupAddr(ctx, ip); e == nil && len(names) > 0 {
		return strings.TrimSuffix(names[0], ".")
	}
	if path, e := exec.LookPath("avahi-resolve-address"); e == nil {
		ctx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		if b, e := exec.CommandContext(ctx, path, "-4", ip).Output(); e == nil {
			fields := strings.Fields(string(b))
			if len(fields) > 1 {
				return fields[1]
			}
		}
	}
	return ""
}
func deviceVendor(mac string) string {
	b, e := net.ParseMAC(mac)
	if e != nil || len(b) != 6 {
		return ""
	}
	if b[0]&2 != 0 {
		return "Случайный / локальный MAC"
	}
	prefix := strings.ToUpper(strings.ReplaceAll(mac[:8], ":", "-"))
	f, e := os.Open(filepath.Join(stateDir(), "db", "oui.txt"))
	if e != nil {
		f, e = os.Open("/usr/share/ieee-data/oui.txt")
	}
	if e != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), prefix) && strings.Contains(line, "(hex)") {
			return strings.TrimSpace(strings.SplitN(line, "(hex)", 2)[1])
		}
	}
	return ""
}
func saveDevice(r *http.Request) error {
	ip := strings.TrimSpace(r.FormValue("ip"))
	if !deviceIP(ip) {
		return fmt.Errorf("Нужен адрес устройства из настроенной подсети LAN")
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) > 200 {
		return fmt.Errorf("Имя слишком длинное")
	}
	target := r.FormValue("target")
	if target != "" {
		if e := validateRuleNode(target); e != nil {
			return e
		}
	}
	deviceLockRecord.Lock()
	defer deviceLockRecord.Unlock()
	var d Device
	json.Unmarshal([]byte(setting("device:"+ip)), &d)
	d.IP = ip
	d.Name = name
	d.Manual = name != ""
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if d.RuleID != 0 {
		var kind, source string
		if tx.QueryRow("SELECT kind,COALESCE((SELECT value FROM settings WHERE key='rule_source:' || rules.id),'') FROM rules WHERE id=?", d.RuleID).Scan(&kind, &source) != nil {
			d.RuleID = 0
		} else if kind != "device" || source != ip {
			return fmt.Errorf("Связанное правило изменено на странице маршрутизации; проверьте его прежде чем менять маршрут устройства")
		}
	}
	if target == "" {
		if d.RuleID != 0 {
			if _, e = tx.Exec("DELETE FROM rules WHERE id=?", d.RuleID); e != nil {
				return e
			}
			if _, e = tx.Exec("DELETE FROM settings WHERE key IN (?,?)", "rule_source:"+strconv.FormatInt(d.RuleID, 10), "rule_disabled:"+strconv.FormatInt(d.RuleID, 10)); e != nil {
				return e
			}
			d.RuleID = 0
		}
	} else {
		label := name
		if label == "" {
			label = d.AutoName
		}
		if label == "" {
			label = ip
		}
		if d.RuleID == 0 {
			res, err := tx.Exec("INSERT INTO rules(priority,name,kind,value,target) SELECT COALESCE(MIN(priority),1)-1,?,'device','',? FROM rules", "Устройство: "+label, target)
			if err != nil {
				return err
			}
			d.RuleID, e = res.LastInsertId()
		} else {
			_, e = tx.Exec("UPDATE rules SET name=?,target=? WHERE id=?", "Устройство: "+label, target, d.RuleID)
		}
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "rule_source:"+strconv.FormatInt(d.RuleID, 10), ip); e != nil {
			return e
		}
	}
	d.Target = target
	b, _ := json.Marshal(d)
	if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "device:"+ip, string(b)); e != nil {
		return e
	}
	return tx.Commit()
}
