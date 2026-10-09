package main

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBulkTargetAtomic(t *testing.T) {
	configDatabase(t)
	db.Exec("INSERT INTO rules VALUES(1,1,'one','domain','example.com','proxy'),(2,2,'two','domain','example.net','proxy')")
	apply := func(ids string) error {
		form := url.Values{"action": {"rule-bulk-target"}, "ids": {ids}, "target": {"direct"}}
		req := httptest.NewRequest("POST", "/action", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, _, err := controlAction(req)
		return err
	}
	if apply("1,999") == nil {
		t.Fatal("missing rule accepted")
	}
	var target string
	db.QueryRow("SELECT target FROM rules WHERE id=1").Scan(&target)
	if target != "proxy" {
		t.Fatal("partial update committed")
	}
	if err := apply("1,2"); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow("SELECT count(*) FROM rules WHERE target='direct'").Scan(&count)
	if count != 2 {
		t.Fatal("bulk update incomplete")
	}
}
