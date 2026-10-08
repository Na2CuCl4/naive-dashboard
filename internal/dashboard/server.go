package dashboard

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Options struct {
	DBPath, Password, KeyDir string
	SecureCookie             bool
	Assets                   fs.FS
	Transport                Transport
}
type session struct {
	CSRF    string
	Expires time.Time
}
type Server struct {
	db        *sql.DB
	mu        sync.Mutex
	authMu    sync.Mutex
	sessions  map[[32]byte]session
	attempts  map[string][]time.Time
	password  []byte
	secure    bool
	transport Transport
	assets    http.Handler
}

func New(o Options) (*Server, error) {
	if len(o.Password) < 16 || len(o.Password) > 72 {
		return nil, errors.New("admin password must be 16..72 bytes")
	}
	db, e := openDB(o.DBPath)
	if e != nil {
		return nil, e
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(o.Password), bcrypt.DefaultCost)
	if e != nil {
		db.Close()
		return nil, e
	}
	if o.Transport == nil {
		o.Transport = SSHTransport{KeyDir: o.KeyDir}
	}
	s := &Server{db: db, password: hash, secure: o.SecureCookie, transport: o.Transport, sessions: map[[32]byte]session{}, attempts: map[string][]time.Time{}}
	if o.Assets != nil {
		s.assets = http.FileServer(http.FS(o.Assets))
	}
	return s, nil
}
func (s *Server) Close() error { return s.db.Close() }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code string) {
	write(w, status, map[string]string{"error": code})
}
func decode(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("invalid_request")
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("invalid_request")
	}
	return nil
}
func (s *Server) getSession(r *http.Request) (session, bool) {
	c, e := r.Cookie("naive_session")
	if e != nil {
		return session{}, false
	}
	key := sha256.Sum256([]byte(c.Value))
	s.authMu.Lock()
	defer s.authMu.Unlock()
	sess, ok := s.sessions[key]
	if ok && time.Now().Before(sess.Expires) {
		return sess, true
	}
	delete(s.sessions, key)
	return session{}, false
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host == r.Host
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if decode(r, &req) != nil || !sameOrigin(r) {
		failure(w, 400, "invalid_request")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.authMu.Lock()
	cut := time.Now().Add(-time.Minute)
	recent := []time.Time{}
	for _, at := range s.attempts[ip] {
		if at.After(cut) {
			recent = append(recent, at)
		}
	}
	if len(recent) >= 10 {
		s.authMu.Unlock()
		failure(w, 429, "rate_limited")
		return
	}
	s.attempts[ip] = append(recent, time.Now())
	if len(s.attempts) > 1000 {
		for key, times := range s.attempts {
			if len(times) == 0 || times[len(times)-1].Before(cut) {
				delete(s.attempts, key)
			}
		}
	}
	s.authMu.Unlock()
	if bcrypt.CompareHashAndPassword(s.password, []byte(req.Password)) != nil {
		failure(w, 401, "unauthorized")
		return
	}
	value := token(32)
	sess := session{token(24), time.Now().Add(12 * time.Hour)}
	s.authMu.Lock()
	for k, v := range s.sessions {
		if !time.Now().Before(v.Expires) {
			delete(s.sessions, k)
		}
	}
	s.sessions[sha256.Sum256([]byte(value))] = sess
	s.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "naive_session", Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	write(w, 200, map[string]any{"authenticated": true, "csrf": sess.CSRF})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" {
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/sub/") {
		s.subscription(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if s.assets == nil {
			http.NotFound(w, r)
			return
		}
		s.assets.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/login" && r.Method == "POST" {
		s.login(w, r)
		return
	}
	sess, ok := s.getSession(r)
	if r.URL.Path == "/api/session" && r.Method == "GET" {
		write(w, 200, map[string]any{"authenticated": ok, "csrf": sess.CSRF})
		return
	}
	if !ok {
		failure(w, 401, "unauthorized")
		return
	}
	if r.Method != "GET" && (subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(sess.CSRF)) != 1 || !sameOrigin(r)) {
		failure(w, 403, "forbidden")
		return
	}
	if r.URL.Path == "/api/logout" && r.Method == "POST" {
		c, _ := r.Cookie("naive_session")
		s.authMu.Lock()
		delete(s.sessions, sha256.Sum256([]byte(c.Value)))
		s.authMu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "naive_session", Value: "", Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	// Read-only views use their own short database snapshot. They must remain
	// available while a serialized write/worker waits for an offline SSH node.
	if r.URL.Path != "/api/state" || r.Method != "GET" {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	var e error
	switch {
	case r.URL.Path == "/api/state" && r.Method == "GET":
		e = s.state(r.Context(), w)
	case strings.HasPrefix(r.URL.Path, "/api/users"):
		e = s.userRoute(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/nodes"):
		e = s.nodeRoute(w, r)
	default:
		failure(w, 404, "not_found")
		return
	}
	if e != nil {
		code := e.Error()
		status := 400
		switch {
		case errors.Is(e, sql.ErrNoRows):
			code = "not_found"
			status = 404
		case code == "invalid_request":
		case code == "conflict" || code == "version_conflict":
			status = 409
		case safeID.MatchString(code):
			status = 502
		default:
			code = "internal_error"
			status = 500
		}
		failure(w, status, code)
	}
}
func (s *Server) state(ctx context.Context, w http.ResponseWriter) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	users, e := readUsers(tx, false)
	if e != nil {
		return e
	}
	nodes, e := readNodes(tx)
	if e != nil {
		return e
	}
	rows, e := tx.Query(`SELECT user_id,node_id,month,up,down FROM usage ORDER BY month DESC`)
	if e != nil {
		return e
	}
	usage := []Usage{}
	for rows.Next() {
		var u Usage
		if e = rows.Scan(&u.UserID, &u.NodeID, &u.Month, &u.Up, &u.Down); e != nil {
			rows.Close()
			return e
		}
		usage = append(usage, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 200, map[string]any{"users": users, "nodes": nodes, "usage": usage, "server_time": now()})
	return nil
}
func (s *Server) userRoute(w http.ResponseWriter, r *http.Request) error {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/users"), "/")
	id := ""
	if len(parts) > 1 {
		id = parts[1]
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/users":
		var u User
		if decode(r, &u) != nil {
			return errors.New("invalid_request")
		}
		if e := s.saveUser(u, true); e != nil {
			return e
		}
	case r.Method == "PUT" && len(parts) == 2 && id != "":
		var u User
		if decode(r, &u) != nil {
			return errors.New("invalid_request")
		}
		u.ID = id
		if e := s.saveUser(u, false); e != nil {
			return e
		}
	case r.Method == "DELETE" && len(parts) == 2 && id != "":
		res, e := s.db.Exec(`UPDATE users SET enabled=0,deleted=1,subscription_token=? WHERE id=? AND deleted=0`, token(32), id)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return sql.ErrNoRows
		}
		s.audit("user.delete", id)
		if e = s.reconcileAll(); e != nil {
			return e
		}
	case r.Method == "POST" && len(parts) == 3 && parts[2] == "rotate":
		var req struct {
			Kind string `json:"kind"`
		}
		if decode(r, &req) != nil {
			return errors.New("invalid_request")
		}
		if e := s.rotate(id, req.Kind); e != nil {
			return e
		}
	default:
		return sql.ErrNoRows
	}
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) nodeRoute(w http.ResponseWriter, r *http.Request) error {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/nodes"), "/")
	id := ""
	if len(parts) > 1 {
		id = parts[1]
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/nodes":
		var n Node
		if decode(r, &n) != nil || validateNode(n) != nil {
			return errors.New("invalid_request")
		}
		n.ID = token(12)
		n.DesiredVersion = 0
		n.AppliedVersion = 0
		n.Health = "unknown"
		n.LastSeen = ""
		n.LastUsage = ""
		n.LastEventAt = ""
		n.UsageBacklog = false
		n.ErrorCode = ""
		n.Cursor = map[string]int64{}
		_, e := s.db.Exec(`INSERT INTO nodes(id,body) VALUES(?,?)`, n.ID, jsonText(n))
		if e != nil {
			return e
		}
		if _, e = s.reconcile(&n); e != nil {
			return e
		}
		s.audit("node.create", n.ID)
	case r.Method == "PUT" && len(parts) == 2 && id != "":
		old, e := s.node(id)
		if e != nil {
			return e
		}
		var n Node
		if decode(r, &n) != nil || validateNode(n) != nil {
			return errors.New("invalid_request")
		}
		old.Name = n.Name
		old.Host = n.Host
		old.SSHPort = n.SSHPort
		old.SSHUser = n.SSHUser
		old.KeyRef = n.KeyRef
		old.HostFingerprint = n.HostFingerprint
		old.ProxyHost = n.ProxyHost
		old.ProxyPort = n.ProxyPort
		old.Protocol = n.Protocol
		old.Health = "unknown"
		if e = s.saveNode(old); e != nil {
			return e
		}
		s.audit("node.update", id)
	case r.Method == "DELETE" && len(parts) == 2 && id != "":
		if _, e := s.node(id); e != nil {
			return e
		}
		var count int
		if e := s.db.QueryRow(`SELECT COUNT(*) FROM grants WHERE node_id=?`, id).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return errors.New("conflict")
		}
		if _, e := s.db.Exec(`DELETE FROM nodes WHERE id=?`, id); e != nil {
			return e
		}
		s.audit("node.delete", id)
	case r.Method == "POST" && len(parts) == 3:
		n, e := s.node(id)
		if e != nil {
			return e
		}
		switch parts[2] {
		case "sync":
			e = s.syncNode(r.Context(), &n)
		case "collect":
			e = s.collectNode(r.Context(), &n)
		case "restart":
			_, e = s.rpc(r.Context(), &n, RPCRequest{Action: "restart"})
		default:
			return sql.ErrNoRows
		}
		s.audit("node."+parts[2], id)
		if e != nil {
			return e
		}
	default:
		return sql.ErrNoRows
	}
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) subscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	tok := strings.TrimPrefix(r.URL.Path, "/sub/")
	if len(tok) != 43 {
		http.NotFound(w, r)
		return
	}
	tx, e := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		failure(w, 500, "internal_error")
		return
	}
	defer tx.Rollback()
	users, e := readUsers(tx, false)
	if e != nil {
		failure(w, 500, "internal_error")
		return
	}
	var user *User
	for i, u := range users {
		if subtle.ConstantTimeCompare([]byte(u.SubscriptionToken), []byte(tok)) == 1 {
			user = &users[i]
		}
	}
	if user == nil || !active(*user) {
		http.NotFound(w, r)
		return
	}
	nodes, e := readNodes(tx)
	if e != nil {
		failure(w, 500, "internal_error")
		return
	}
	lines := []string{}
	for _, n := range nodes {
		var c Credential
		e = tx.QueryRow(`SELECT username,password FROM grants WHERE user_id=? AND node_id=? AND enabled=1`, user.ID, n.ID).Scan(&c.Username, &c.Password)
		if e == sql.ErrNoRows {
			continue
		}
		if e != nil {
			failure(w, 500, "internal_error")
			return
		}
		u := url.URL{Scheme: "naive+" + n.Protocol, User: url.UserPassword(c.Username, c.Password), Host: net.JoinHostPort(n.ProxyHost, fmt.Sprint(n.ProxyPort)), Fragment: n.Name}
		lines = append(lines, u.String())
	}
	if e = tx.Commit(); e != nil {
		failure(w, 500, "internal_error")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")
	io.WriteString(w, strings.Join(lines, "\n")+"\n")
}
func ReadPassword(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}
func Shutdown(ctx context.Context, h *http.Server) { _ = h.Shutdown(ctx) }
