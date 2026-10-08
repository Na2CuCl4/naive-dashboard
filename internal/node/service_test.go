package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const testTemplate = `{
 admin 127.0.0.1:2019
}
:8443 {
 forward_proxy {
  # NAIVE_USERS
  hide_ip
  hide_via
  probe_resistance
 }
 respond "unchanged site {body}"
}
`

func fakeService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	usageDir := filepath.Join(dir, "usage")
	for _, path := range []string{stateDir, usageDir} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(dir, "active")
	old, err := renderTemplate(testTemplate, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, active, old)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/" {
			t.Errorf("unexpected admin path: %s", r.URL.Path)
		}
		data, err := os.ReadFile(active)
		if err != nil {
			http.Error(w, "not available", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"config": string(data)})
	}))
	t.Cleanup(server.Close)
	cfg := Config{
		CaddyBinary: filepath.Join(dir, "caddy"), CaddyConfig: filepath.Join(dir, "Caddyfile"),
		TemplateFile: filepath.Join(dir, "Caddyfile.template"), StateDir: stateDir, UsageDir: usageDir,
		AdminAddress: strings.TrimPrefix(server.URL, "http://"),
	}
	writeTest(t, cfg.CaddyConfig, old)
	writeTest(t, cfg.TemplateFile, []byte(testTemplate))
	script := fmt.Sprintf(`#!/usr/bin/python3
import json, pathlib, sys
base = pathlib.Path(%q)
action = sys.argv[1]
config = pathlib.Path(sys.argv[sys.argv.index('--config')+1]).read_text()
if action == 'adapt':
 print(json.dumps({'config':config}))
elif action == 'validate':
 sys.exit(1 if '# REJECT' in config else 0)
elif action == 'reload':
 with (base/'calls').open('a') as log: log.write('reload\n')
 if (base/'fail-all').exists(): sys.exit(1)
 if '# NO_APPLY' not in config: (base/'active').write_text(config)
 if '# FAIL_RELOAD' in config: sys.exit(1)
else:
 sys.exit(1)
`, dir)
	writeTest(t, cfg.CaddyBinary, []byte(script))
	if err := os.Chmod(cfg.CaddyBinary, 0700); err != nil {
		t.Fatal(err)
	}
	return New(cfg), dir
}

func writeTest(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func applyRequest(version int64) Request {
	return Request{Action: "apply", Version: version, Users: []User{{Username: "u_demo_n_one", Password: "abcDEF0123456789_-"}}}
}

func TestApplyIdempotencyRevocationAndDrift(t *testing.T) {
	s, dir := fakeService(t)
	if response := s.Handle(Request{Action: "status"}); !response.OK || response.Health != "unmanaged" {
		t.Fatalf("bootstrap status: %+v", response)
	}
	request := applyRequest(1)
	if response := s.Handle(request); !response.OK || response.AppliedVersion != 1 {
		t.Fatalf("apply: %+v", response)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if response := s.Handle(request); !response.OK {
		t.Fatalf("retry: %+v", response)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !bytes.Equal(before, after) {
		t.Fatal("idempotent retry reloaded Caddy")
	}
	request.Users[0].Password = "DIFFERENT_0123456789"
	if response := s.Handle(request); response.Error != "version_conflict" {
		t.Fatalf("version reuse: %+v", response)
	}
	if response := s.Handle(Request{Action: "apply", Version: 2}); !response.OK {
		t.Fatalf("revoke last user: %+v", response)
	}
	data, _ := os.ReadFile(s.config.CaddyConfig)
	if strings.Contains(string(data), "u_demo_n_one") || !strings.Contains(string(data), "basic_auth deny_") || !strings.Contains(string(data), `respond "unchanged site {body}"`) {
		t.Fatal("zero-user authentication or unrelated site was not preserved")
	}
	if response := s.Handle(applyRequest(1)); response.Error != "version_conflict" {
		t.Fatalf("stale version: %+v", response)
	}
	writeTest(t, filepath.Join(dir, "active"), []byte("unexpected running config"))
	if response := s.Handle(Request{Action: "status"}); response.Error != "runtime_drift" {
		t.Fatalf("runtime drift: %+v", response)
	}
	writeTest(t, s.config.CaddyConfig, []byte("manual disk edit"))
	if response := s.Handle(Request{Action: "status"}); response.Error != "config_drift" {
		t.Fatalf("disk drift: %+v", response)
	}
}

func TestApplyFailuresPreserveDiskRuntimeAndVersion(t *testing.T) {
	for _, test := range []struct{ marker, code string }{
		{"REJECT", "validation_failed"},
		{"FAIL_RELOAD", "reload_failed"},
		{"NO_APPLY", "runtime_mismatch"},
	} {
		t.Run(test.marker, func(t *testing.T) {
			s, dir := fakeService(t)
			if response := s.Handle(applyRequest(1)); !response.OK {
				t.Fatal(response)
			}
			before, _ := os.ReadFile(s.config.CaddyConfig)
			writeTest(t, s.config.TemplateFile, []byte(testTemplate+"# "+test.marker+"\n"))
			if response := s.Handle(applyRequest(2)); response.Error != test.code || response.AppliedVersion != 1 {
				t.Fatalf("apply failure: %+v", response)
			}
			for _, path := range []string{s.config.CaddyConfig, filepath.Join(dir, "active")} {
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatalf("previous config not restored: %s", path)
				}
			}
			if response := s.Handle(Request{Action: "status"}); !response.OK || response.AppliedVersion != 1 {
				t.Fatalf("post-rollback status: %+v", response)
			}
		})
	}
}

func TestRecoveryAfterInterruptedInstall(t *testing.T) {
	s, dir := fakeService(t)
	if response := s.Handle(applyRequest(1)); !response.OK {
		t.Fatal(response)
	}
	previous, _ := os.ReadFile(s.config.CaddyConfig)
	oldState, err := s.readState()
	if err != nil {
		t.Fatal(err)
	}
	journal, _ := json.Marshal(transaction{PreviousConfig: previous, PreviousState: oldState, RuntimeHash: oldState.RuntimeHash})
	writeTest(t, s.statePath("transaction.json"), journal)
	writeTest(t, s.config.CaddyConfig, []byte("half applied"))
	writeTest(t, filepath.Join(dir, "active"), []byte("half applied"))
	writeTest(t, filepath.Join(dir, "fail-all"), []byte(""))
	if response := s.Handle(Request{Action: "status"}); response.Error != "recovery_failed" {
		t.Fatalf("unavailable Caddy during recovery: %+v", response)
	}
	if _, err := os.Stat(s.statePath("transaction.json")); err != nil {
		t.Fatal("failed recovery removed durable journal")
	}
	if err := os.Remove(filepath.Join(dir, "fail-all")); err != nil {
		t.Fatal(err)
	}
	if response := s.Handle(Request{Action: "status"}); !response.OK || response.AppliedVersion != 1 {
		t.Fatalf("recovery retry: %+v", response)
	}
	if _, err := os.Stat(s.statePath("transaction.json")); !os.IsNotExist(err) {
		t.Fatal("recovered transaction not removed")
	}
}

func TestStrictRequestsAndLocks(t *testing.T) {
	for _, body := range []string{
		`{"action":"shell"}`, `{"action":"status","command":"id"}`,
		`{"action":"status"}{"action":"restart"}`,
		`{"action":"apply","version":1,"users":[{"username":"bad\nuser","password":"abcdefgh01234567"}]}`,
		`{"action":"apply","version":1,"users":[{"username":"u","password":"{$PASSWORD_ENV}"}]}`,
		`{"action":"usage","cursor":{"../../etc/shadow":0}}`,
		strings.Repeat(" ", MaxRequestBytes+1),
	} {
		if _, err := DecodeRequest(strings.NewReader(body)); err == nil {
			t.Fatalf("accepted invalid RPC %q", body[:min(100, len(body))])
		}
	}
	s, _ := fakeService(t)
	lock, err := os.OpenFile(s.statePath("lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if response := s.Handle(Request{Action: "status"}); response.Error != "node_busy" {
		t.Fatalf("concurrent RPC: %+v", response)
	}
}

func TestTemplateBoundaries(t *testing.T) {
	for _, value := range []string{
		":8443 {\n # NAIVE_USERS\n}\n",
		testTemplate + "# NAIVE_USERS\n",
		strings.Replace(testTemplate, "# NAIVE_USERS", "basic_auth fixed password\n # NAIVE_USERS", 1),
	} {
		if _, err := renderTemplate(value, nil); err == nil {
			t.Fatal("accepted unsafe template")
		}
	}
	otherSite := ":9443 {\n forward_proxy {\n  basic_auth separate unchanged\n }\n}\n"
	result, err := renderTemplate(testTemplate+otherSite, nil)
	if err != nil || !strings.HasSuffix(string(result), otherSite) {
		t.Fatal("unrelated site was changed", err)
	}
}
