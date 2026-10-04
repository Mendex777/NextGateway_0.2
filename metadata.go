package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SubscriptionInfo struct {
	Upload, Download, Total, Expire *int64
	Title, Message                  string
}

func headerText(value string) string {
	if strings.HasPrefix(value, "base64:") {
		raw := strings.TrimPrefix(value, "base64:")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, e := enc.DecodeString(raw); e == nil {
				value = string(b)
				break
			}
		}
	}
	if len(value) > 4096 {
		value = value[:4096]
	}
	return value
}
func subscriptionInfo(h http.Header) SubscriptionInfo {
	info := SubscriptionInfo{Title: headerText(h.Get("Profile-Title")), Message: headerText(h.Get("Profile-Notice"))}
	if info.Message == "" {
		info.Message = headerText(h.Get("Announce"))
	}
	for _, part := range strings.Split(h.Get("Subscription-Userinfo"), ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		v, e := strconv.ParseInt(strings.TrimSpace(pair[1]), 10, 64)
		if e != nil || v < 0 {
			continue
		}
		switch strings.ToLower(pair[0]) {
		case "upload":
			info.Upload = &v
		case "download":
			info.Download = &v
		case "total":
			info.Total = &v
		case "expire":
			info.Expire = &v
		}
	}
	return info
}
func bytesText(v int64) string {
	if v < 1024 {
		return fmt.Sprintf("%d Б", v)
	}
	units := []string{"КиБ", "МиБ", "ГиБ", "ТиБ", "ПиБ", "ЭиБ"}
	n := float64(v)
	for _, unit := range units {
		n /= 1024
		if n < 1024 {
			return fmt.Sprintf("%.2f %s", n, unit)
		}
	}
	return fmt.Sprintf("%.2f ЭиБ", n)
}
func fillSourceInfo(s *Source, now time.Time) {
	var info SubscriptionInfo
	json.Unmarshal([]byte(setting(fmt.Sprintf("sub_info:%d", s.ID))), &info)
	s.ProviderTitle = info.Title
	s.ProviderMessage = info.Message
	s.Usage = "Нет данных"
	s.Limit = "Нет данных"
	s.Expires = "Нет данных"
	s.NextUpdate = "Выключено"
	if info.Upload != nil && info.Download != nil {
		if *info.Upload <= 9223372036854775807-*info.Download {
			s.Usage = bytesText(*info.Upload + *info.Download)
		} else {
			s.Usage = bytesText(*info.Upload) + " отправлено + " + bytesText(*info.Download) + " получено"
		}
	}
	if info.Total != nil {
		if *info.Total == 0 {
			s.Limit = "Без лимита"
		} else {
			s.Limit = bytesText(*info.Total)
		}
	}
	if info.Expire != nil {
		if *info.Expire == 0 {
			s.Expires = "Не указан"
		} else {
			date := time.Unix(*info.Expire, 0).In(time.FixedZone("MSK", 3*3600))
			s.Expires = date.Format("02.01.2006 15:04") + " МСК"
			if !date.After(now) {
				s.Expires += " (истекла)"
			}
		}
	}
	if s.Interval >= 15 {
		last, e := time.Parse(time.RFC3339, setting(fmt.Sprintf("sub_attempt:%d", s.ID)))
		next := last.Add(time.Duration(s.Interval) * time.Minute)
		if e != nil || !next.After(now) {
			s.NextUpdate = "Ожидает обновления (проверка очереди раз в минуту)"
		} else {
			s.NextUpdate = next.In(time.FixedZone("MSK", 3*3600)).Format("02.01.2006 15:04") + " МСК"
		}
	}
}
