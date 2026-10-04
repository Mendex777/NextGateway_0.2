package main

import (
	"net/http"
	"testing"
	"time"
)

func TestSubscriptionInfoMissingAndInvalidValues(t *testing.T) {
	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=0; download=1024; total=0; expire=0")
	h.Set("Profile-Title", "base64:VGVzdA==")
	h.Set("Announce", "base64:VGVzdA==")
	info := subscriptionInfo(h)
	if info.Title != "Test" || info.Message != "Test" || info.Total == nil || *info.Total != 0 || *info.Download != 1024 {
		t.Fatal("metadata parsing failed")
	}
	h.Set("Subscription-Userinfo", "upload=-1; download=bad; total=9223372036854775808")
	info = subscriptionInfo(h)
	if info.Upload != nil || info.Download != nil || info.Total != nil {
		t.Fatal("invalid values accepted")
	}
	configDatabase(t)
	s := Source{ID: 1, Interval: 15}
	fillSourceInfo(&s, time.Now())
	if s.Usage != "Нет данных" || s.Limit != "Нет данных" || s.NextUpdate == "Выключено" {
		t.Fatal("missing metadata or schedule shown incorrectly")
	}
}
