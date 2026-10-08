package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"time"
)

const MaxRequestBytes = 1 << 20
const MaxUsers = 2000

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Request struct {
	Action  string           `json:"action"`
	Version int64            `json:"version,omitempty"`
	Users   []User           `json:"users,omitempty"`
	Cursor  map[string]int64 `json:"cursor,omitempty"`
}

// Event contains cumulative application payload bytes, excluding Naive padding.
// A (node, epoch, session_id) must be accounted by monotonic deltas, never sums.
type Event struct {
	Epoch     string    `json:"epoch"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Up        int64     `json:"up"`
	Down      int64     `json:"down"`
	Final     bool      `json:"final"`
	At        time.Time `json:"at"`
}

type Response struct {
	OK             bool             `json:"ok"`
	Error          string           `json:"error,omitempty"`
	AppliedVersion int64            `json:"applied_version"`
	Health         string           `json:"health,omitempty"`
	Events         []Event          `json:"events,omitempty"`
	Cursor         map[string]int64 `json:"cursor"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var passwordPattern = regexp.MustCompile(`^[A-Za-z0-9_.~-]{16,256}$`)

func DecodeRequest(r io.Reader) (Request, error) {
	var request Request
	data, err := io.ReadAll(io.LimitReader(r, MaxRequestBytes+1))
	if err != nil || len(data) > MaxRequestBytes {
		return request, errors.New("invalid_request")
	}
	if err := decodeStrict(data, &request); err != nil {
		return request, err
	}
	return request, request.validate()
}

func (r Request) validate() error {
	switch r.Action {
	case "apply":
		if r.Version <= 0 || len(r.Users) > MaxUsers || len(r.Cursor) != 0 {
			return errors.New("invalid_request")
		}
		seen := make(map[string]bool, len(r.Users))
		for _, user := range r.Users {
			if !usernamePattern.MatchString(user.Username) || !passwordPattern.MatchString(user.Password) || seen[user.Username] {
				return errors.New("invalid_request")
			}
			seen[user.Username] = true
		}
	case "usage":
		if r.Version != 0 || len(r.Users) != 0 || len(r.Cursor) > 10000 {
			return errors.New("invalid_request")
		}
		for name, offset := range r.Cursor {
			if !journalName.MatchString(name) || offset < 0 {
				return errors.New("invalid_request")
			}
		}
	case "status", "restart":
		if r.Version != 0 || len(r.Users) != 0 || len(r.Cursor) != 0 {
			return errors.New("invalid_request")
		}
	default:
		return errors.New("invalid_request")
	}
	return nil
}

func sortedUsers(users []User) []User {
	result := append([]User{}, users...)
	sort.Slice(result, func(i, j int) bool { return result[i].Username < result[j].Username })
	return result
}

func decodeStrict(data []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("trailing_json")
	}
	return nil
}
