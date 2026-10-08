package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type waitingTransport struct {
	entered chan struct{}
	release chan struct{}
}

func (f *waitingTransport) Call(ctx context.Context, _ Node, _ RPCRequest) (RPCResponse, error) {
	close(f.entered)
	select {
	case <-ctx.Done():
		return RPCResponse{}, ctx.Err()
	case <-f.release:
		return RPCResponse{}, io.ErrUnexpectedEOF
	}
}

func TestReadViewsRemainAvailableDuringSSHWait(t *testing.T) {
	s, n, u, _ := setup(t)
	login := call(s, "POST", "/api/login", `{"password":"test-password-long-enough"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	cookie := login.Result().Cookies()[0].String()
	transport := &waitingTransport{entered: make(chan struct{}), release: make(chan struct{})}
	s.transport = transport
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = s.syncNode(context.Background(), &n)
	}()
	t.Cleanup(func() { close(transport.release); <-done })
	select {
	case <-transport.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sync never entered SSH wait")
	}
	for _, path := range []string{"/api/state", "/sub/" + u.SubscriptionToken} {
		result := make(chan *httptest.ResponseRecorder, 1)
		go func(path string) { result <- call(s, "GET", path, "", cookie, "") }(path)
		select {
		case response := <-result:
			if response.Code != 200 {
				t.Fatalf("read while SSH waits: %s returned %d: %s", path, response.Code, response.Body.String())
			}
			if strings.HasPrefix(path, "/sub/") && !strings.Contains(response.Body.String(), "naive+https://") {
				t.Fatal("subscription lost granted proxy")
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("read %s blocked behind SSH write lock", path)
		}
	}
}

func TestStateIsReadOnlyAndExpiredSubscriptionIsRejected(t *testing.T) {
	s, n, u, username := setup(t)
	if err := s.ingest(&n, RPCResponse{Events: []Event{event(username, "snapshot", now(), 12, 30, false)}, Cursor: map[string]int64{}}); err != nil {
		t.Fatal(err)
	}
	// Model time crossing an expiration between worker cycles. GET must expose
	// persisted state without assigning config versions or authorizing expiry.
	if _, err := s.db.Exec(`UPDATE users SET expires_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), u.ID); err != nil {
		t.Fatal(err)
	}
	login := call(s, "POST", "/api/login", `{"password":"test-password-long-enough"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	response := call(s, "GET", "/api/state", "", login.Result().Cookies()[0].String(), "")
	var body struct {
		Users []User  `json:"users"`
		Nodes []Node  `json:"nodes"`
		Usage []Usage `json:"usage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || len(body.Users) != 1 || len(body.Nodes) != 1 || len(body.Usage) != 1 {
		t.Fatalf("state response: %s (%v)", response.Body.String(), err)
	}
	if body.Users[0].TotalUp != body.Usage[0].Up || body.Users[0].TotalDown != body.Usage[0].Down {
		t.Fatal("state omitted usage rows or totals")
	}
	stored, err := s.node(n.ID)
	if err != nil || stored.DesiredVersion != n.DesiredVersion || body.Nodes[0].DesiredVersion != n.DesiredVersion {
		t.Fatal("GET assigned a new config version", err)
	}
	if response := call(s, "GET", "/sub/"+u.SubscriptionToken, "", "", ""); response.Code != 404 {
		t.Fatal("expired user received subscription before next worker cycle")
	}
}
