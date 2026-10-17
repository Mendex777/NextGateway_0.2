package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDeviceRouteAndManualName(t *testing.T) {
	configDatabase(t)
	save := func(name, target string) error {
		r := httptest.NewRequest("POST", "/action", nil)
		r.Form = url.Values{"ip": {"192.168.1.56"}, "name": {name}, "target": {target}}
		return saveDevice(r)
	}
	if e := save("Телефон", "direct"); e != nil {
		t.Fatal(e)
	}
	rows := devices()
	if len(rows) != 1 || rows[0].Name != "Телефон" || rows[0].RuleID == 0 {
		t.Fatal("device not saved")
	}
	id := rows[0].RuleID
	if source := setting("rule_source:" + strconvID(id)); source != "192.168.1.56" {
		t.Fatal("rule source missing")
	}
	if e := save("Телефон новый", "block"); e != nil {
		t.Fatal(e)
	}
	if devices()[0].RuleID != id {
		t.Fatal("route edit duplicated rule")
	}
	var d Device
	json.Unmarshal([]byte(setting("device:192.168.1.56")), &d)
	d.AutoName = "automatic"
	b, _ := json.Marshal(d)
	saveSetting("device:192.168.1.56", string(b))
	if devices()[0].Name != "Телефон новый" {
		t.Fatal("automatic name overwrote manual")
	}
	if e := save("", ""); e != nil {
		t.Fatal(e)
	}
	if devices()[0].Name != "automatic" || devices()[0].RuleID != 0 {
		t.Fatal("automatic name or inherited routing not restored")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM rules").Scan(&count)
	if count != 0 {
		t.Fatal("managed rule left behind")
	}
	if deviceIP("192.168.1.1") || deviceIP("8.8.8.8") {
		t.Fatal("non-client accepted")
	}
}
func strconvID(id int64) string { return fmt.Sprint(id) }

func TestMapRenamePreservesDeviceRoute(t *testing.T) {
	configDatabase(t)
	r := httptest.NewRequest("POST", "/api/action", nil)
	r.Form = url.Values{"ip": {"192.168.1.56"}, "name": {"Phone"}, "target": {"block"}}
	if err := saveDevice(r); err != nil {
		t.Fatal(err)
	}
	before := devices()[0]
	var ruleName, target string
	if err := db.QueryRow("SELECT name,target FROM rules WHERE id=?", before.RuleID).Scan(&ruleName, &target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"My phone", ""} {
		r.Form.Set("name", name)
		if err := saveDeviceName(r); err != nil {
			t.Fatal(err)
		}
		after := devices()[0]
		var gotName, gotTarget string
		db.QueryRow("SELECT name,target FROM rules WHERE id=?", before.RuleID).Scan(&gotName, &gotTarget)
		if after.RuleID != before.RuleID || after.Target != before.Target || gotName != ruleName || gotTarget != target {
			t.Fatal("renaming rewrote routing")
		}
		if after.Manual != (name != "") {
			t.Fatal("manual name state not saved")
		}
	}
	r.Form.Set("ip", "8.8.8.8")
	if saveDeviceName(r) == nil {
		t.Fatal("outside-LAN address accepted")
	}
}
