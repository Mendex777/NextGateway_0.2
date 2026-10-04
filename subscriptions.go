package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var subscriptionLock sync.Mutex

func canonicalURI(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return raw
	}
	u.Fragment = ""
	u.RawFragment = ""
	u.RawQuery = u.Query().Encode()
	return u.String()
}
func saveSource(r *http.Request) error {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 200 {
		return fmt.Errorf("Укажите название источника")
	}
	raw := strings.TrimSpace(r.FormValue("url"))
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("Нужен HTTP(S) URL")
	}
	var headers map[string]string
	if json.Unmarshal([]byte(r.FormValue("headers")), &headers) != nil || headers == nil {
		return fmt.Errorf("Заголовки должны быть JSON-объектом")
	}
	for k, v := range headers {
		if k == "" || strings.ContainsAny(k, " :\r\n\t") || strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("Некорректный HTTP-заголовок")
		}
	}
	interval := 0
	if r.FormValue("interval") != "" {
		interval, e = strconv.Atoi(r.FormValue("interval"))
		if e != nil || interval != 0 && (interval < 15 || interval > 10080) {
			return fmt.Errorf("Интервал: 0 либо от 15 до 10080 минут")
		}
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	if r.FormValue("action") == "source-update" {
		if e = tx.QueryRow("SELECT id FROM sources WHERE id=? AND url!=?", r.FormValue("id"), "manual:").Scan(&id); e == nil {
			_, e = tx.Exec("UPDATE sources SET name=?,url=?,headers=? WHERE id=?", name, raw, r.FormValue("headers"), id)
		}
	} else {
		res, err := tx.Exec("INSERT INTO sources(name,url,headers) VALUES(?,?,?)", name, raw, r.FormValue("headers"))
		e = err
		if e == nil {
			id, e = res.LastInsertId()
		}
	}
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", fmt.Sprintf("sub_interval:%d", id), strconv.Itoa(interval))
	if e != nil {
		return e
	}
	return tx.Commit()
}
func refreshRecorded(id string) error {
	saveSetting("sub_attempt:"+id, time.Now().UTC().Format(time.RFC3339))
	e := refresh(id)
	if e != nil {
		db.Exec("UPDATE sources SET error=? WHERE id=?", e.Error(), id)
	}
	return e
}
func subscriptionWorker() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		refreshDueSources(now)
	}
}
func refreshDueSources(now time.Time) {
	for _, s := range sources() {
		if s.URL == "manual:" {
			continue
		}
		id := strconv.Itoa(s.ID)
		minutes, _ := strconv.Atoi(setting("sub_interval:" + id))
		if minutes < 15 {
			continue
		}
		last, _ := time.Parse(time.RFC3339, setting("sub_attempt:"+id))
		if now.Sub(last) >= time.Duration(minutes)*time.Minute {
			refreshRecorded(id)
		}
	}
}
