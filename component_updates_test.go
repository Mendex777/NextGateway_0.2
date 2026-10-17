package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeoReleaseChecksBothDigests(t *testing.T) {
	dir := t.TempDir()
	r := updateRelease{}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		data := []byte(name)
		_ = os.WriteFile(filepath.Join(dir, name), data, 0600)
		h := sha256.Sum256(data)
		r.Assets = append(r.Assets, struct{ Name, Digest string }{name, "sha256:" + hex.EncodeToString(h[:])})
	}
	different, err := geoReleaseDiffers(r, dir)
	if err != nil || different {
		t.Fatal(different, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "geosite.dat"), []byte("old"), 0600)
	different, err = geoReleaseDiffers(r, dir)
	if err != nil || !different {
		t.Fatal("modified geo not detected", err)
	}
	r.Assets[0].Digest = ""
	if _, err = geoReleaseDiffers(r, dir); err == nil {
		t.Fatal("missing published digest accepted")
	}
}
func TestUpdatesSnapshotExpires(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	s := ComponentUpdates{Updated: time.Now().Add(-25 * time.Hour), Components: []ComponentUpdate{{ID: "geo", Available: true}}}
	raw, _ := json.Marshal(s)
	_ = os.Mkdir(filepath.Join(dir, "db"), 0700)
	_ = os.WriteFile(filepath.Join(dir, "db/component-updates.json"), raw, 0600)
	got := componentUpdatesSnapshot()
	if got.Components[0].Available || got.Components[0].Error == "" {
		t.Fatal("stale update advertised")
	}
}

type releaseTransport struct{ url string }

func (r releaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u, _ := http.NewRequest("GET", r.url, nil)
	clone.URL = u.URL
	return http.DefaultTransport.RoundTrip(clone)
}
func TestReleaseRejectsErrorsAndPrerelease(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v26.3.28","prerelease":true}`, `{"tag_name":"v26.3.28","draft":true}`, `{broken`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		old := releaseClient
		releaseClient = &http.Client{Transport: releaseTransport{server.URL}}
		_, err := fetchUpdateRelease("XTLS/Xray-core")
		releaseClient = old
		server.Close()
		if err == nil {
			t.Fatal("invalid release accepted", body)
		}
	}
}

func TestCoreUpdateRequestIsCompleteAndExclusive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NG_STATE", dir)
	_ = os.Mkdir(filepath.Join(dir, "jobs"), 0700)
	if err := requestCoreInstall(true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "jobs/install.request"))
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]bool
	if json.Unmarshal(raw, &job) != nil || !job["update"] {
		t.Fatal("incomplete update request", string(raw))
	}
	if err = requestCoreInstall(false); err == nil {
		t.Fatal("overwrote running job")
	}
	if operationKind("xray-update") != "install" {
		t.Fatal("update is not monitored")
	}
}
