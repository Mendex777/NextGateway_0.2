package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ComponentUpdate struct {
	ID, Current, Latest, Error string
	Available                  bool
}
type ComponentUpdates struct {
	Updated      time.Time
	Busy         bool
	Components   []ComponentUpdate
	GeoSignature string
}
type updateRelease struct {
	Tag               string `json:"tag_name"`
	Draft, Prerelease bool
	Assets            []struct{ Name, Digest string }
}

var componentUpdateLock sync.Mutex
var componentUpdateBusy bool
var releaseClient = &http.Client{Timeout: 12 * time.Second}

func fetchUpdateRelease(repo string) (updateRelease, error) {
	var r updateRelease
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "NGPanel")
	response, err := releaseClient.Do(req)
	if err != nil {
		return r, fmt.Errorf("Проверка обновлений недоступна")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return r, fmt.Errorf("GitHub: HTTP %d", response.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&r) != nil || r.Tag == "" || len(r.Tag) > 100 || r.Draft || r.Prerelease {
		return r, fmt.Errorf("Некорректный стабильный выпуск")
	}
	return r, nil
}
func geoCurrent() string {
	b, _ := os.ReadFile("/usr/local/share/ngpanel-geodata/source.txt")
	parts := strings.Fields(string(b))
	if len(parts) > 1 && parts[0] == "Loyalsoldier/v2ray-rules-dat" {
		return strings.TrimSuffix(parts[1], ";")
	}
	return "Нет версии"
}
func geoReleaseDiffers(r updateRelease, dir string) (bool, error) {
	changed := false
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		digest := ""
		for _, a := range r.Assets {
			if a.Name == name {
				digest = a.Digest
			}
		}
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
			return false, fmt.Errorf("Нет контрольной суммы %s", name)
		}
		if _, err := hex.DecodeString(digest[7:]); err != nil {
			return false, fmt.Errorf("Некорректная контрольная сумма")
		}
		f, err := os.Open(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			changed = true
			continue
		}
		if err != nil {
			return false, fmt.Errorf("База %s недоступна", name)
		}
		hash := sha256.New()
		_, err = io.Copy(hash, io.LimitReader(f, 64*1024*1024+1))
		_ = f.Close()
		if err != nil {
			return false, fmt.Errorf("База %s не прочитана", name)
		}
		changed = changed || hex.EncodeToString(hash.Sum(nil)) != digest[7:]
	}
	return changed, nil
}
func componentUpdatesSnapshot() ComponentUpdates {
	var s ComponentUpdates
	b, _ := os.ReadFile(filepath.Join(stateDir(), "db", "component-updates.json"))
	if len(b) < 65536 {
		_ = json.Unmarshal(b, &s)
	}
	componentUpdateLock.Lock()
	s.Busy = componentUpdateBusy
	componentUpdateLock.Unlock()
	for i := range s.Components {
		c := &s.Components[i]
		switch c.ID {
		case "panel":
			c.Current = panelVersion
			c.Available = c.Error == "" && newerCore(c.Latest, panelVersion)
		case "xray":
			for _, check := range readServiceHealth().Checks {
				if check.ID == "xray" {
					fields := strings.Fields(check.Actual)
					if len(fields) > 0 && newerCore(fields[len(fields)-1], "0.0.0") {
						c.Current = fields[len(fields)-1]
					}
				}
			}
			c.Available = c.Error == "" && newerCore(c.Latest, c.Current)
		}
		if c.ID == "geo" {
			c.Current = geoCurrent()
			if s.GeoSignature != geoInstalledSignature() {
				c.Available = false
				c.Error = "Проверяем установленный выпуск баз"
			}
		}
		if time.Since(s.Updated) > 24*time.Hour {
			c.Error = "Сведения об обновлениях устарели"
			c.Available = false
		}
	}
	return s
}
func refreshComponentUpdates() {
	s := ComponentUpdates{Updated: time.Now().UTC(), Components: []ComponentUpdate{}, GeoSignature: geoInstalledSignature()}
	version, _ := healthCommand("/usr/local/bin/xray", "version")
	parts := strings.Fields(string(version))
	core := "Не установлен"
	if len(parts) > 1 && parts[0] == "Xray" {
		core = parts[1]
	}
	for _, item := range []struct{ id, repo, current string }{{"panel", "Mendex777/NextGateway_0.2", panelVersion}, {"xray", "XTLS/Xray-core", core}, {"geo", "Loyalsoldier/v2ray-rules-dat", geoCurrent()}} {
		c := ComponentUpdate{ID: item.id, Current: item.current}
		r, err := fetchUpdateRelease(item.repo)
		if err == nil {
			c.Latest = r.Tag
			if item.id == "geo" {
				c.Available, err = geoReleaseDiffers(r, "/usr/local/share/ngpanel-geodata")
			} else if !newerCore(r.Tag, "0.0.0") {
				err = fmt.Errorf("Некорректная версия выпуска")
			} else {
				c.Available = newerCore(r.Tag, item.current)
			}
		}
		if err != nil {
			c.Error = err.Error()
			c.Available = false
		}
		s.Components = append(s.Components, c)
	}
	s.Updated = time.Now().UTC()
	_ = os.MkdirAll(filepath.Join(stateDir(), "db"), 0700)
	raw, _ := json.Marshal(s)
	f, err := os.CreateTemp(filepath.Join(stateDir(), "db"), ".component-updates-")
	if err == nil {
		_ = f.Chmod(0600)
		_, err = f.Write(raw)
		_ = f.Close()
		if err == nil {
			_ = os.Rename(f.Name(), filepath.Join(stateDir(), "db", "component-updates.json"))
		}
		_ = os.Remove(f.Name())
	}
}
func requestComponentUpdateCheck() bool {
	componentUpdateLock.Lock()
	if componentUpdateBusy {
		componentUpdateLock.Unlock()
		return false
	}
	componentUpdateBusy = true
	componentUpdateLock.Unlock()
	go func() {
		defer func() { componentUpdateLock.Lock(); componentUpdateBusy = false; componentUpdateLock.Unlock() }()
		refreshComponentUpdates()
	}()
	return true
}
func componentUpdateWorker() {
	for {
		if s := componentUpdatesSnapshot(); s.Updated.IsZero() || time.Since(s.Updated) > 6*time.Hour || s.GeoSignature != geoInstalledSignature() {
			requestComponentUpdateCheck()
		}
		time.Sleep(time.Minute)
	}
}

func geoInstalledSignature() string {
	sig := ""
	for _, name := range []string{"geosite.dat", "geoip.dat"} {
		if info, err := os.Stat(filepath.Join("/usr/local/share/ngpanel-geodata", name)); err == nil {
			sig += fmt.Sprint(name, info.Size(), info.ModTime().UnixNano(), info.Mode())
		}
	}
	return sig
}
func requestCoreInstall(update bool) error {
	dir := filepath.Join(stateDir(), "jobs")
	f, err := os.CreateTemp(dir, ".xray-install-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if update {
		err = json.NewEncoder(f).Encode(map[string]bool{"update": true})
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), filepath.Join(dir, "install.request")); err != nil {
		return fmt.Errorf("Установка Xray уже выполняется или очередь недоступна")
	}
	return nil
}
