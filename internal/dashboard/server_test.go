package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTransport struct {
	calls   []RPCRequest
	version int64
	events  []Event
	cursor  map[string]int64
	fail    bool
}

func (f *fakeTransport) Call(_ context.Context, _ Node, r RPCRequest) (RPCResponse, error) {
	f.calls = append(f.calls, r)
	if f.fail {
		return RPCResponse{}, io.ErrUnexpectedEOF
	}
	if r.Action == "apply" {
		f.version = r.Version
	}
	return RPCResponse{OK: true, Health: "healthy", AppliedVersion: f.version, Events: f.events, Cursor: f.cursor}, nil
}
func setup(t *testing.T) (*Server, Node, User, string) {
	t.Helper()
	f := &fakeTransport{cursor: map[string]int64{}}
	s, e := New(Options{DBPath: filepath.Join(t.TempDir(), "test.db"), Password: "test-password-long-enough", Transport: f})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	n := Node{ID: "node1", Name: "Tokyo", Host: "localhost", SSHPort: 22, SSHUser: "naive", KeyRef: "node1", HostFingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ProxyHost: "proxy.test", ProxyPort: 443, Protocol: "https", Health: "unknown", Cursor: map[string]int64{}}
	if _, e = s.db.Exec(`INSERT INTO nodes(id,body) VALUES(?,?)`, n.ID, jsonText(n)); e != nil {
		t.Fatal(e)
	}
	if e = s.saveUser(User{Name: "Alice", Enabled: true, Grants: []string{n.ID}}, true); e != nil {
		t.Fatal(e)
	}
	users, e := s.users(false)
	if e != nil {
		t.Fatal(e)
	}
	n, e = s.node(n.ID)
	if e != nil {
		t.Fatal(e)
	}
	var username string
	if e = s.db.QueryRow(`SELECT username FROM grants WHERE user_id=?`, users[0].ID).Scan(&username); e != nil {
		t.Fatal(e)
	}
	return s, n, users[0], username
}
func event(username, sid, at string, up, down int64, final bool) Event {
	return Event{Epoch: "epoch1", SessionID: sid, UserID: username, At: at, Up: up, Down: down, Final: final}
}
func totals(t *testing.T, s *Server, uid string) (int64, int64) {
	t.Helper()
	var up, down int64
	if e := s.db.QueryRow(`SELECT COALESCE(SUM(up),0),COALESCE(SUM(down),0) FROM usage WHERE user_id=?`, uid).Scan(&up, &down); e != nil {
		t.Fatal(e)
	}
	return up, down
}
func TestCumulativeReplayRestartAndMonths(t *testing.T) {
	s, n, u, name := setup(t)
	jan := "2026-01-31T23:59:30Z"
	feb := "2026-02-01T00:00:30Z"
	first := event(name, "long", jan, 100, 200, false)
	batch := func(events []Event, offset int64) {
		t.Helper()
		if e := s.ingest(&n, RPCResponse{Events: events, Cursor: map[string]int64{"epoch1.jsonl": offset}}); e != nil {
			t.Fatal(e)
		}
	}
	batch([]Event{first}, 100)
	batch([]Event{first}, 100)
	batch([]Event{event(name, "long", feb, 150, 260, true), first, event(name, "short", feb, 7, 9, true)}, 300)
	if up, down := totals(t, s, u.ID); up != 157 || down != 269 {
		t.Fatalf("got %d %d", up, down)
	}
	var finalized bool
	s.db.QueryRow(`SELECT final FROM sessions WHERE session_id='long'`).Scan(&finalized)
	if !finalized {
		t.Fatal("old snapshot cleared final")
	}
	var up int64
	if e := s.db.QueryRow(`SELECT up FROM usage WHERE month='2026-02'`).Scan(&up); e != nil || up != 57 {
		t.Fatalf("month delta %d %v", up, e)
	}
	second := first
	second.Epoch = "epoch2"
	second.Final = true
	batch([]Event{second}, 400)
	if up, down := totals(t, s, u.ID); up != 257 || down != 469 {
		t.Fatal(up, down)
	}
	// Database reopen plus full replay preserves cursors and cumulative totals.
	path := ""
	rows, e := s.db.Query(`PRAGMA database_list`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var seq int
		var dbname string
		rows.Scan(&seq, &dbname, &path)
	}
	rows.Close()
	s.db.Close()
	s.db, e = openDB(path)
	if e != nil {
		t.Fatal(e)
	}
	n, e = s.node(n.ID)
	if e != nil || n.Cursor["epoch1.jsonl"] != 400 {
		t.Fatal(n, e)
	}
	batch([]Event{first, second}, 400)
	if up, down := totals(t, s, u.ID); up != 257 || down != 469 {
		t.Fatal(up, down)
	}
}
func TestInvalidBatchRollsBackCursorAndUsage(t *testing.T) {
	s, n, u, name := setup(t)
	good := event(name, "s1", now(), 12, 34, true)
	bad := good
	bad.UserID = "unknown"
	if e := s.ingest(&n, RPCResponse{Events: []Event{good, bad}, Cursor: map[string]int64{"epoch1.jsonl": 500}}); e == nil {
		t.Fatal("accepted unknown identity")
	}
	if up, down := totals(t, s, u.ID); up != 0 || down != 0 {
		t.Fatal("partial commit")
	}
	stored, _ := s.node(n.ID)
	if len(stored.Cursor) > 0 {
		t.Fatal("advanced cursor")
	}
	if e := s.ingest(&n, RPCResponse{Events: []Event{good}, Cursor: map[string]int64{"epoch1.jsonl": 500}}); e != nil {
		t.Fatal(e)
	}
	if e := s.ingest(&n, RPCResponse{Cursor: map[string]int64{}}); e == nil {
		t.Fatal("accepted cursor rewind/missing file")
	}
}
func TestRotationExpiryQuotaAndOldIdentity(t *testing.T) {
	s, n, u, old := setup(t)
	creds, _ := s.desired(n)
	if len(creds) != 1 {
		t.Fatal(creds)
	}
	original := creds[0]
	if e := s.rotate(u.ID, "subscription"); e != nil {
		t.Fatal(e)
	}
	creds, _ = s.desired(n)
	if creds[0] != original {
		t.Fatal("subscription rotation changed proxy")
	}
	users, _ := s.users(false)
	if users[0].SubscriptionToken == u.SubscriptionToken {
		t.Fatal("token unchanged")
	}
	u = users[0]
	if e := s.rotate(u.ID, "proxy"); e != nil {
		t.Fatal(e)
	}
	creds, _ = s.desired(n)
	if creds[0] == original {
		t.Fatal("proxy unchanged")
	}
	if e := s.ingest(&n, RPCResponse{Events: []Event{event(old, "old-session", now(), 50, 50, true)}, Cursor: map[string]int64{}}); e != nil {
		t.Fatal("old identity lost", e)
	}
	u.QuotaBytes = 100
	if e := s.saveUser(u, false); e != nil {
		t.Fatal(e)
	}
	creds, _ = s.desired(n)
	if len(creds) != 0 {
		t.Fatal("quota not revoked")
	}
	stored, _ := s.node(n.ID)
	if stored.DesiredVersion <= stored.AppliedVersion {
		t.Fatal("revocation not pending")
	}
	u.QuotaBytes = 200
	if e := s.saveUser(u, false); e != nil {
		t.Fatal(e)
	}
	creds, _ = s.desired(n)
	if len(creds) != 1 {
		t.Fatal("increased quota did not restore")
	}
	u.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	s.saveUser(u, false)
	creds, _ = s.desired(n)
	if len(creds) != 0 {
		t.Fatal("expiry not revoked")
	}
}
func call(s *Server, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestAuthCSRFAndSubscription(t *testing.T) {
	s, _, u, _ := setup(t)
	if w := call(s, "GET", "/api/state", "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	login := call(s, "POST", "/api/login", `{"password":"test-password-long-enough"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie not hardened")
	}
	var body struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(login.Body.Bytes(), &body)
	w := call(s, "POST", "/api/users/"+u.ID+"/rotate", `{"kind":"subscription"}`, cookie.String(), "")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	sub := call(s, "GET", "/sub/"+u.SubscriptionToken, "", "", "")
	if sub.Code != 200 || !strings.Contains(sub.Body.String(), "naive+https://") || strings.Contains(sub.Body.String(), "clash") {
		t.Fatal(sub.Code, sub.Body.String())
	}
	w = call(s, "POST", "/api/users/"+u.ID+"/rotate", `{"kind":"subscription"}`, cookie.String(), body.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call(s, "GET", "/sub/"+u.SubscriptionToken, "", "", ""); w.Code != 404 {
		t.Fatal("old token still valid")
	}
	users, _ := s.users(false)
	u = users[0]
	u.Enabled = false
	s.saveUser(u, false)
	if w = call(s, "GET", "/sub/"+u.SubscriptionToken, "", "", ""); w.Code != 404 {
		t.Fatal("disabled user subscription")
	}
}
func TestSyncPersistsPendingWhenOffline(t *testing.T) {
	s, n, u, _ := setup(t)
	f := s.transport.(*fakeTransport)
	if e := s.syncNode(context.Background(), &n); e != nil {
		t.Fatal(e)
	}
	if n.AppliedVersion != n.DesiredVersion {
		t.Fatal(n)
	}
	u.Enabled = false
	s.saveUser(u, false)
	n, _ = s.node(n.ID)
	f.fail = true
	if e := s.syncNode(context.Background(), &n); e == nil {
		t.Fatal("offline accepted")
	}
	n, _ = s.node(n.ID)
	if n.Health != "offline" || n.AppliedVersion >= n.DesiredVersion {
		t.Fatal("lost pending revocation")
	}
	f.fail = false
	if e := s.syncNode(context.Background(), &n); e != nil {
		t.Fatal(e)
	}
	last := f.calls[len(f.calls)-1]
	if last.Action != "apply" || len(last.Users) != 0 {
		t.Fatal("did not revoke")
	}
}
func TestGrantSaveIsAtomic(t *testing.T) {
	s, _, u, _ := setup(t)
	u.Grants = []string{"missing"}
	if e := s.saveUser(u, false); e == nil {
		t.Fatal("invalid grant accepted")
	}
	var count int
	e := s.db.QueryRow(`SELECT COUNT(*) FROM grants WHERE enabled=1`).Scan(&count)
	if e != nil || count != 1 {
		t.Fatal("previous grants lost", count, e)
	}
	_, e = s.node("missing")
	if e != sql.ErrNoRows {
		t.Fatal(e)
	}
}
