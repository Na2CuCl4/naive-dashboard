package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"time"
)

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=FULL")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`
 CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY,name TEXT NOT NULL,enabled INTEGER NOT NULL,deleted INTEGER NOT NULL DEFAULT 0,expires_at TEXT NOT NULL,quota_bytes INTEGER NOT NULL,subscription_token TEXT NOT NULL UNIQUE);
 CREATE TABLE IF NOT EXISTS nodes(id TEXT PRIMARY KEY,body TEXT NOT NULL,cursor TEXT NOT NULL DEFAULT '{}',config_hash TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS grants(user_id TEXT NOT NULL REFERENCES users(id),node_id TEXT NOT NULL REFERENCES nodes(id),username TEXT NOT NULL,password TEXT NOT NULL,enabled INTEGER NOT NULL,PRIMARY KEY(user_id,node_id));
 CREATE TABLE IF NOT EXISTS identities(username TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),node_id TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(node_id TEXT NOT NULL,epoch TEXT NOT NULL,session_id TEXT NOT NULL,user_id TEXT NOT NULL,up INTEGER NOT NULL,down INTEGER NOT NULL,final INTEGER NOT NULL,last_at TEXT NOT NULL,PRIMARY KEY(node_id,epoch,session_id));
 CREATE TABLE IF NOT EXISTS usage(user_id TEXT NOT NULL,node_id TEXT NOT NULL,month TEXT NOT NULL,up INTEGER NOT NULL,down INTEGER NOT NULL,PRIMARY KEY(user_id,node_id,month));
 CREATE TABLE IF NOT EXISTS audit(id INTEGER PRIMARY KEY AUTOINCREMENT,at TEXT NOT NULL,action TEXT NOT NULL,object_id TEXT NOT NULL);
 `)
	if e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}

// queryer allows related reads to share a single SQLite snapshot. In particular,
// never call s.db while a transaction holds the only configured connection.
type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func (s *Server) users(includeDeleted bool) ([]User, error) {
	return readUsers(s.db, includeDeleted)
}

func readUsers(db queryer, includeDeleted bool) ([]User, error) {
	q := `SELECT u.id,u.name,u.enabled,u.expires_at,u.quota_bytes,u.subscription_token,COALESCE((SELECT SUM(up) FROM usage WHERE user_id=u.id),0),COALESCE((SELECT SUM(down) FROM usage WHERE user_id=u.id),0) FROM users u`
	if !includeDeleted {
		q += " WHERE deleted=0"
	}
	q += " ORDER BY name,id"
	rows, e := db.Query(q)
	if e != nil {
		return nil, e
	}
	out := []User{}
	for rows.Next() {
		var u User
		e = rows.Scan(&u.ID, &u.Name, &u.Enabled, &u.ExpiresAt, &u.QuotaBytes, &u.SubscriptionToken, &u.TotalUp, &u.TotalDown)
		if e != nil {
			rows.Close()
			return nil, e
		}
		u.QuotaState = quota(u)
		u.Grants = []string{}
		out = append(out, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = db.Query(`SELECT user_id,node_id FROM grants WHERE enabled=1 ORDER BY node_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	index := map[string]int{}
	for i, u := range out {
		index[u.ID] = i
	}
	for rows.Next() {
		var uid, nid string
		if e = rows.Scan(&uid, &nid); e != nil {
			return nil, e
		}
		if i, ok := index[uid]; ok {
			out[i].Grants = append(out[i].Grants, nid)
		}
	}
	return out, rows.Err()
}
func (s *Server) nodes() ([]Node, error) {
	return readNodes(s.db)
}

func readNodes(db queryer) ([]Node, error) {
	rows, e := db.Query(`SELECT body,cursor,config_hash FROM nodes ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var b, c, h string
		if e = rows.Scan(&b, &c, &h); e != nil {
			return nil, e
		}
		var n Node
		if e = json.Unmarshal([]byte(b), &n); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(c), &n.Cursor); e != nil {
			return nil, e
		}
		n.ConfigHash = h
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Server) node(id string) (Node, error) {
	var b, c, h string
	e := s.db.QueryRow(`SELECT body,cursor,config_hash FROM nodes WHERE id=?`, id).Scan(&b, &c, &h)
	if e != nil {
		return Node{}, e
	}
	var n Node
	if e = json.Unmarshal([]byte(b), &n); e != nil {
		return n, e
	}
	e = json.Unmarshal([]byte(c), &n.Cursor)
	n.ConfigHash = h
	return n, e
}
func (s *Server) saveNode(n Node) error {
	_, e := s.db.Exec(`UPDATE nodes SET body=?,cursor=?,config_hash=? WHERE id=?`, jsonText(n), jsonText(n.Cursor), n.ConfigHash, n.ID)
	return e
}
func (s *Server) audit(action, id string) {
	s.db.Exec(`INSERT INTO audit(at,action,object_id) VALUES(?,?,?)`, now(), action, id)
}
func (s *Server) saveUser(u User, create bool) error {
	if e := validateUser(u); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if create {
		u.ID = token(12)
		u.SubscriptionToken = token(32)
		_, e = tx.Exec(`INSERT INTO users(id,name,enabled,expires_at,quota_bytes,subscription_token) VALUES(?,?,?,?,?,?)`, u.ID, u.Name, u.Enabled, u.ExpiresAt, u.QuotaBytes, u.SubscriptionToken)
	} else {
		var found int
		e = tx.QueryRow(`SELECT 1 FROM users WHERE id=? AND deleted=0`, u.ID).Scan(&found)
		if e == nil {
			_, e = tx.Exec(`UPDATE users SET name=?,enabled=?,expires_at=?,quota_bytes=? WHERE id=?`, u.Name, u.Enabled, u.ExpiresAt, u.QuotaBytes, u.ID)
		}
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE grants SET enabled=0 WHERE user_id=?`, u.ID); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, nid := range u.Grants {
		if seen[nid] {
			continue
		}
		seen[nid] = true
		var exists int
		if e = tx.QueryRow(`SELECT 1 FROM nodes WHERE id=?`, nid).Scan(&exists); e != nil {
			return errors.New("invalid_request")
		}
		var username string
		e = tx.QueryRow(`SELECT username FROM grants WHERE user_id=? AND node_id=?`, u.ID, nid).Scan(&username)
		if e == sql.ErrNoRows {
			username = token(18)
			_, e = tx.Exec(`INSERT INTO grants VALUES(?,?,?,?,1)`, u.ID, nid, username, token(32))
			if e == nil {
				_, e = tx.Exec(`INSERT INTO identities VALUES(?,?,?)`, username, u.ID, nid)
			}
		} else if e == nil {
			_, e = tx.Exec(`UPDATE grants SET enabled=1 WHERE user_id=? AND node_id=?`, u.ID, nid)
		}
		if e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.audit("user.save", u.ID)
	return s.reconcileAll()
}
func (s *Server) rotate(uid, kind string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var found int
	if e = tx.QueryRow(`SELECT 1 FROM users WHERE id=? AND deleted=0`, uid).Scan(&found); e != nil {
		return e
	}
	switch kind {
	case "subscription":
		_, e = tx.Exec(`UPDATE users SET subscription_token=? WHERE id=?`, token(32), uid)
	case "proxy":
		rows, err := tx.Query(`SELECT node_id FROM grants WHERE user_id=?`, uid)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, nid := range ids {
			username := token(18)
			_, e = tx.Exec(`UPDATE grants SET username=?,password=? WHERE user_id=? AND node_id=?`, username, token(32), uid, nid)
			if e != nil {
				return e
			}
			_, e = tx.Exec(`INSERT INTO identities VALUES(?,?,?)`, username, uid, nid)
			if e != nil {
				return e
			}
		}
	default:
		return errors.New("invalid_request")
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.audit("user.rotate."+kind, uid)
	return s.reconcileAll()
}

// ingest atomically advances cumulative high-water marks and the transport cursor.
// Independent maxima tolerate out-of-order snapshots without double accounting.
func (s *Server) ingest(n *Node, r RPCResponse) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, v := range r.Events {
		at, err := time.Parse(time.RFC3339Nano, v.At)
		if err != nil || at.After(time.Now().Add(5*time.Minute)) || !safeID.MatchString(v.Epoch) || !safeID.MatchString(v.SessionID) || !safeID.MatchString(v.UserID) || v.Up < 0 || v.Down < 0 || v.Up > 1<<60 || v.Down > 1<<60 {
			return errors.New("usage_invalid")
		}
		var uid string
		if err = tx.QueryRow(`SELECT user_id FROM identities WHERE username=? AND node_id=?`, v.UserID, n.ID).Scan(&uid); err != nil {
			return errors.New("usage_invalid")
		}
		var oldU, oldD int64
		var oldFinal bool
		var oldUID, oldAt string
		err = tx.QueryRow(`SELECT user_id,up,down,final,last_at FROM sessions WHERE node_id=? AND epoch=? AND session_id=?`, n.ID, v.Epoch, v.SessionID).Scan(&oldUID, &oldU, &oldD, &oldFinal, &oldAt)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if oldUID != "" && oldUID != uid {
			return errors.New("usage_invalid")
		}
		up, down := v.Up, v.Down
		if up < oldU {
			up = oldU
		}
		if down < oldD {
			down = oldD
		}
		latest := v.At
		if oldAt != "" {
			t, _ := time.Parse(time.RFC3339Nano, oldAt)
			if t.After(at) {
				latest = oldAt
			}
		}
		_, e = tx.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id,epoch,session_id) DO UPDATE SET up=excluded.up,down=excluded.down,final=excluded.final,last_at=excluded.last_at`, n.ID, v.Epoch, v.SessionID, uid, up, down, oldFinal || v.Final, latest)
		if e != nil {
			return e
		}
		if up > oldU || down > oldD {
			_, e = tx.Exec(`INSERT INTO usage VALUES(?,?,?,?,?) ON CONFLICT(user_id,node_id,month) DO UPDATE SET up=usage.up+excluded.up,down=usage.down+excluded.down`, uid, n.ID, at.UTC().Format("2006-01"), up-oldU, down-oldD)
			if e != nil {
				return e
			}
		}
	}
	if r.Cursor == nil {
		return errors.New("usage_invalid")
	}
	for file, offset := range n.Cursor {
		if r.Cursor[file] < offset {
			return errors.New("usage_invalid")
		}
	}
	for file, offset := range r.Cursor {
		if offset < 0 || !safeRef.MatchString(file) {
			return errors.New("usage_invalid")
		}
	}
	next := *n
	next.Cursor = r.Cursor
	next.LastUsage = now()
	next.UsageBacklog = len(r.Events) >= 1000
	latestEvent, _ := time.Parse(time.RFC3339Nano, next.LastEventAt)
	for _, event := range r.Events {
		observed, _ := time.Parse(time.RFC3339Nano, event.At)
		if observed.After(latestEvent) {
			latestEvent = observed
			next.LastEventAt = event.At
		}
	}
	_, e = tx.Exec(`UPDATE nodes SET body=?,cursor=? WHERE id=?`, jsonText(next), jsonText(next.Cursor), n.ID)
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	*n = next
	return nil
}
