package dashboard

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type User struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	ExpiresAt         string   `json:"expires_at"`
	QuotaBytes        int64    `json:"quota_bytes"`
	SubscriptionToken string   `json:"subscription_token"`
	TotalUp           int64    `json:"total_up"`
	TotalDown         int64    `json:"total_down"`
	QuotaState        string   `json:"quota_state"`
	Grants            []string `json:"grants"`
}
type Node struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Host            string           `json:"host"`
	SSHPort         int              `json:"ssh_port"`
	SSHUser         string           `json:"ssh_user"`
	KeyRef          string           `json:"key_ref"`
	HostFingerprint string           `json:"host_fingerprint"`
	ProxyHost       string           `json:"proxy_host"`
	ProxyPort       int              `json:"proxy_port"`
	Protocol        string           `json:"protocol"`
	DesiredVersion  int64            `json:"desired_version"`
	AppliedVersion  int64            `json:"applied_version"`
	Health          string           `json:"health"`
	LastSeen        string           `json:"last_seen"`
	LastUsage       string           `json:"last_usage"`
	LastEventAt     string           `json:"last_event_at"`
	UsageBacklog    bool             `json:"usage_backlog"`
	ErrorCode       string           `json:"error_code"`
	Cursor          map[string]int64 `json:"-"`
	ConfigHash      string           `json:"-"`
}
type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type Event struct {
	Epoch     string `json:"epoch"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Up        int64  `json:"up"`
	Down      int64  `json:"down"`
	Final     bool   `json:"final"`
	At        string `json:"at"`
}
type RPCRequest struct {
	Action  string           `json:"action"`
	Version int64            `json:"version,omitempty"`
	Users   []Credential     `json:"users,omitempty"`
	Cursor  map[string]int64 `json:"cursor,omitempty"`
}
type RPCResponse struct {
	OK             bool             `json:"ok"`
	Error          string           `json:"error"`
	AppliedVersion int64            `json:"applied_version"`
	Health         string           `json:"health"`
	Events         []Event          `json:"events"`
	Cursor         map[string]int64 `json:"cursor"`
}
type Usage struct {
	UserID string `json:"user_id"`
	NodeID string `json:"node_id"`
	Month  string `json:"month"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
}

var safeRef = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}$`)
var safeID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,160}$`)

func token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func validHost(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if len(p) == 0 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return false
		}
		for _, c := range p {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}
func validateNode(n Node) error {
	fp, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(n.HostFingerprint, "SHA256:"))
	if strings.TrimSpace(n.Name) == "" || utf8.RuneCountInString(n.Name) > 100 || !validHost(n.Host) || !validHost(n.ProxyHost) || n.SSHPort < 1 || n.SSHPort > 65535 || n.ProxyPort < 1 || n.ProxyPort > 65535 || !safeRef.MatchString(n.SSHUser) || !safeRef.MatchString(n.KeyRef) || strings.Contains(n.KeyRef, "..") || !strings.HasPrefix(n.HostFingerprint, "SHA256:") || err != nil || len(fp) != 32 || (n.Protocol != "https" && n.Protocol != "quic") {
		return errors.New("invalid_request")
	}
	return nil
}
func validateUser(u User) error {
	if strings.TrimSpace(u.Name) == "" || utf8.RuneCountInString(u.Name) > 100 || u.QuotaBytes < 0 || u.QuotaBytes > 1<<60 {
		return errors.New("invalid_request")
	}
	if u.ExpiresAt != "" {
		if _, e := time.Parse(time.RFC3339, u.ExpiresAt); e != nil {
			return errors.New("invalid_request")
		}
	}
	return nil
}
func active(u User) bool {
	if !u.Enabled {
		return false
	}
	if u.ExpiresAt != "" {
		t, e := time.Parse(time.RFC3339, u.ExpiresAt)
		if e != nil || !t.After(time.Now()) {
			return false
		}
	}
	return u.QuotaBytes == 0 || u.TotalUp+u.TotalDown < u.QuotaBytes
}
func quota(u User) string {
	total := u.TotalUp + u.TotalDown
	if u.QuotaBytes > 0 {
		if total >= u.QuotaBytes {
			return "exceeded"
		}
		if float64(total) >= float64(u.QuotaBytes)*0.9 {
			return "near"
		}
	}
	return "normal"
}
func jsonText(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(fmt.Sprint(e))
	}
	return string(b)
}
