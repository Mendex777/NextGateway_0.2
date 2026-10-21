package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func storedRouteDevice(t *testing.T, ip string) Device {
	t.Helper()
	var d Device
	if err := json.Unmarshal([]byte(setting("device:"+ip)), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func saveRouteDevice(t *testing.T, ip, target string) Device {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/action", nil)
	r.Form = url.Values{"ip": {ip}, "name": {"Phone"}, "target": {target}}
	if err := saveDevice(r); err != nil {
		t.Fatal(err)
	}
	return storedRouteDevice(t, ip)
}
func staleDeviceFixture(t *testing.T) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO rules VALUES(14,1,'google','ip','geoip:google','direct')"); err != nil {
		t.Fatal(err)
	}
	if err := saveSetting("device:192.168.1.56", `{"IP":"192.168.1.56","Name":"Phone","MAC":"aa:bb:cc:dd:ee:ff","Manual":true,"RuleID":14,"Target":"block","TargetLabel":"old","Extra":"keep"}`); err != nil {
		t.Fatal(err)
	}
}
func googleRule(t *testing.T) []string {
	t.Helper()
	values := make([]string, 5)
	if err := db.QueryRow("SELECT name,kind,value,target,COALESCE((SELECT value FROM settings WHERE key='rule_source:14'),'') FROM rules WHERE id=14").Scan(&values[0], &values[1], &values[2], &values[3], &values[4]); err != nil {
		t.Fatal(err)
	}
	return values
}
func TestStaleDeviceRepairPreservesForeignRule(t *testing.T) {
	configDatabase(t)
	staleDeviceFixture(t)
	before := googleRule(t)
	view := devices()[0]
	if view.RuleID != 0 || view.Target != "" {
		t.Fatal("Foreign rule shown as device route", view)
	}
	if err := repairDeviceRoutes(); err != nil {
		t.Fatal(err)
	}
	d := storedRouteDevice(t, "192.168.1.56")
	if d.RuleID != 0 || d.Target != "" || d.TargetLabel != "" || d.Name != "Phone" || d.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatal(d)
	}
	var fields map[string]any
	json.Unmarshal([]byte(setting("device:192.168.1.56")), &fields)
	if fields["Extra"] != "keep" || !reflect.DeepEqual(before, googleRule(t)) {
		t.Fatal("Repair changed unrelated data")
	}
	if err := repairDeviceRoutes(); err != nil {
		t.Fatal(err)
	}
}
func TestStaleDeviceSaveCreatesOwnedRule(t *testing.T) {
	configDatabase(t)
	staleDeviceFixture(t)
	before := googleRule(t)
	d := saveRouteDevice(t, "192.168.1.56", "block")
	if d.RuleID == 14 || d.RuleID == 0 {
		t.Fatal(d)
	}
	if _, _, err := ownedDeviceRoute(db, d.RuleID, d.IP); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, googleRule(t)) {
		t.Fatal("Saving device overwrote Google rule")
	}
}
func TestDeletedDeviceRuleCannotBeReused(t *testing.T) {
	for _, action := range []string{"rule-delete", "rule-bulk-delete"} {
		t.Run(action, func(t *testing.T) {
			configDatabase(t)
			d := saveRouteDevice(t, "192.168.1.56", "direct")
			id := strconv.FormatInt(d.RuleID, 10)
			if err := ruleAction(t, url.Values{"action": {action}, "id": {id}, "ids": {id}}); err != nil {
				t.Fatal(err)
			}
			if after := storedRouteDevice(t, d.IP); after.RuleID != 0 || after.Target != "" {
				t.Fatal("Deleted association persisted", after)
			}
			if _, err := db.Exec("INSERT INTO rules VALUES(?,1,'replacement','domain','example.org','block')", d.RuleID); err != nil {
				t.Fatal(err)
			}
			after := saveRouteDevice(t, d.IP, "direct")
			if after.RuleID == d.RuleID {
				t.Fatal("Reused foreign ID overwritten")
			}
		})
	}
}
func TestImportReplaceDetachesBeforeRuleIDReuse(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(strconv.FormatBool(replace), func(t *testing.T) {
			configDatabase(t)
			d := saveRouteDevice(t, "192.168.1.56", "direct")
			template := routeTemplate{Format: "ngpanel-routing", Version: 1, Default: "direct", Rules: []routeTemplateRule{{Name: "replacement", Kind: "device", Source: d.IP, Target: "block"}}}
			if _, err := importRoutes(template, nil, nil, replace, false, orderString(allRules())); err != nil {
				t.Fatal(err)
			}
			after := storedRouteDevice(t, d.IP)
			if replace {
				if after.RuleID != 0 || after.Target != "" {
					t.Fatal("Imported rule inherited old association", after)
				}
			} else if after.RuleID != d.RuleID || after.Target != d.Target {
				t.Fatal("Append detached valid route", after)
			}
		})
	}
}
func TestImportFailureRollsBackDeviceDetach(t *testing.T) {
	configDatabase(t)
	d := saveRouteDevice(t, "192.168.1.56", "direct")
	db.Exec("CREATE TRIGGER reject_fixture BEFORE INSERT ON rules BEGIN SELECT RAISE(ABORT,'fixture rejection'); END")
	template := routeTemplate{Format: "ngpanel-routing", Version: 1, Default: "direct", Rules: []routeTemplateRule{{Name: "replacement", Kind: "domain", Value: "example.org", Target: "direct"}}}
	if _, err := importRoutes(template, nil, nil, true, false, orderString(allRules())); err == nil {
		t.Fatal("Failed import accepted")
	}
	if after := storedRouteDevice(t, d.IP); after.RuleID != d.RuleID || after.Target != d.Target {
		t.Fatal("Rollback lost association", after)
	}
	if _, _, err := ownedDeviceRoute(db, d.RuleID, d.IP); err != nil {
		t.Fatal(err)
	}
}
func TestEditedDeviceRuleDetachesAssociation(t *testing.T) {
	configDatabase(t)
	d := saveRouteDevice(t, "192.168.1.56", "direct")
	if err := ruleAction(t, url.Values{"action": {"rule-update"}, "id": {strconv.FormatInt(d.RuleID, 10)}, "name": {"user rule"}, "kind": {"domain"}, "value": {"example.org"}, "target": {"block"}}); err != nil {
		t.Fatal(err)
	}
	if after := storedRouteDevice(t, d.IP); after.RuleID != 0 || after.Target != "" {
		t.Fatal("Edited rule still owned", after)
	}
}
func TestDeviceRepairOnDatabaseCopy(t *testing.T) {
	path := os.Getenv("NG_DEVICE_REPAIR_COPY")
	if path == "" {
		t.Skip("Copied production database fixture")
	}
	if filepath.Base(path) != "panel-copy.db" {
		t.Fatal("Expected explicit database copy")
	}
	var err error
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	before := googleRule(t)
	if err = repairDeviceRoutes(); err != nil {
		t.Fatal(err)
	}
	d := storedRouteDevice(t, "192.168.1.56")
	if d.RuleID != 0 || d.Target != "" {
		t.Fatal(d)
	}
	if !reflect.DeepEqual(before, googleRule(t)) {
		t.Fatal("Copy repair changed Google")
	}
	saveRouteDevice(t, d.IP, "direct")
	if !reflect.DeepEqual(before, googleRule(t)) {
		t.Fatal("Copy device save changed Google")
	}
	t.Log("Production copy: repair and device save passed; Google unchanged")
}
