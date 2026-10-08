package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

func (s *Server) desired(n Node) ([]Credential, error) {
	users, e := s.users(false)
	if e != nil {
		return nil, e
	}
	allowed := map[string]bool{}
	for _, u := range users {
		allowed[u.ID] = active(u)
	}
	rows, e := s.db.Query(`SELECT user_id,username,password FROM grants WHERE node_id=? AND enabled=1 ORDER BY username`, n.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var uid string
		var c Credential
		if e = rows.Scan(&uid, &c.Username, &c.Password); e != nil {
			return nil, e
		}
		if allowed[uid] {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}
func (s *Server) reconcile(n *Node) ([]Credential, error) {
	creds, e := s.desired(*n)
	if e != nil {
		return nil, e
	}
	hash := sha256.Sum256([]byte(jsonText(creds)))
	h := hex.EncodeToString(hash[:])
	if n.ConfigHash != h {
		n.ConfigHash = h
		n.DesiredVersion++
		if e = s.saveNode(*n); e != nil {
			return nil, e
		}
	}
	return creds, nil
}
func (s *Server) reconcileAll() error {
	nodes, e := s.nodes()
	if e != nil {
		return e
	}
	for _, n := range nodes {
		if _, e = s.reconcile(&n); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) rpc(ctx context.Context, n *Node, req RPCRequest) (RPCResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, e := s.transport.Call(ctx, *n, req)
	if e != nil {
		n.Health = "offline"
		if r.Error != "" {
			n.Health = "error"
			n.LastSeen = now()
			n.AppliedVersion = r.AppliedVersion
		}
		n.ErrorCode = e.Error()
		if !safeID.MatchString(n.ErrorCode) {
			n.ErrorCode = "node_error"
		}
		s.saveNode(*n)
		return r, e
	}
	n.LastSeen = now()
	n.AppliedVersion = r.AppliedVersion
	if r.Health != "" {
		n.Health = r.Health
		if n.Health != "healthy" {
			n.Health = "error"
		}
	}
	n.ErrorCode = ""
	if e = s.saveNode(*n); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Server) syncNode(ctx context.Context, n *Node) error {
	creds, e := s.reconcile(n)
	if e != nil {
		return e
	}
	r, e := s.rpc(ctx, n, RPCRequest{Action: "status"})
	if e != nil {
		return e
	}
	if n.AppliedVersion > n.DesiredVersion {
		n.Health = "error"
		n.ErrorCode = "version_conflict"
		s.saveNode(*n)
		return errors.New("version_conflict")
	}
	if n.AppliedVersion != n.DesiredVersion || r.Health != "healthy" {
		r, e = s.rpc(ctx, n, RPCRequest{Action: "apply", Version: n.DesiredVersion, Users: creds})
		if e != nil {
			return e
		}
		if r.AppliedVersion != n.DesiredVersion || r.Health != "healthy" {
			n.Health = "error"
			n.ErrorCode = "node_error"
			s.saveNode(*n)
			return errors.New("node_error")
		}
	}
	return nil
}
func (s *Server) collectNode(ctx context.Context, n *Node) error {
	r, e := s.rpc(ctx, n, RPCRequest{Action: "usage", Cursor: n.Cursor})
	if e != nil {
		return e
	}
	if e = s.ingest(n, r); e != nil {
		n.Health = "error"
		n.ErrorCode = "usage_invalid"
		s.saveNode(*n)
		return e
	}
	return s.reconcileAll()
}
func (s *Server) RunWorker(ctx context.Context) {
	// First cycle reconciles on startup, then each minute. Serialize against admin writes.
	s.cycle(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cycle(ctx)
		}
	}
}
func (s *Server) cycle(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes, e := s.nodes()
	if e != nil {
		return
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, n := range nodes {
		if ctx.Err() != nil {
			return
		}
		_ = s.collectNode(ctx, &n)
		current, e := s.node(n.ID)
		if e == nil {
			_ = s.syncNode(ctx, &current)
		}
	}
}
