package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Service struct {
	config Config
}

type state struct {
	Version     int64  `json:"version"`
	RequestHash string `json:"request_hash"`
	ConfigHash  string `json:"config_hash"`
	RuntimeHash string `json:"runtime_hash"`
}

type transaction struct {
	PreviousConfig []byte `json:"previous_config"`
	PreviousState  *state `json:"previous_state,omitempty"`
	RuntimeHash    string `json:"runtime_hash"`
}

func New(config Config) *Service { return &Service{config: config} }

func (s *Service) Handle(request Request) Response {
	if request.validate() != nil {
		return failure("invalid_request", 0)
	}
	if s.config.validate() != nil {
		return failure("invalid_node_config", 0)
	}
	lock, err := os.OpenFile(filepath.Join(s.config.StateDir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return failure("state_unavailable", 0)
	}
	defer lock.Close()
	// A busy node can be retried; never hold an SSH worker indefinitely.
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return failure("node_busy", 0)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err = s.recover(); err != nil {
		return failure("recovery_failed", 0)
	}
	current, err := s.readState()
	if err != nil {
		return failure("state_unavailable", 0)
	}
	version := int64(0)
	if current != nil {
		version = current.Version
	}
	switch request.Action {
	case "status":
		return s.status(current)
	case "apply":
		return s.apply(request, current)
	case "usage":
		events, cursor, err := readUsage(s.config.UsageDir, request.Cursor)
		if err != nil {
			return failure(err.Error(), version)
		}
		return Response{OK: true, AppliedVersion: version, Events: events, Cursor: cursor}
	case "restart":
		if len(s.config.RestartCommand) == 0 {
			return failure("restart_disabled", version)
		}
		// Refuse to turn an unacknowledged manual edit into running state.
		if current != nil {
			data, err := os.ReadFile(s.config.CaddyConfig)
			if err != nil || digest(data) != current.ConfigHash {
				return failure("config_drift", version)
			}
		}
		if _, err := s.command(s.config.RestartCommand[0], s.config.RestartCommand[1:]...); err != nil {
			return failure("restart_failed", version)
		}
		return s.status(current)
	}
	return failure("invalid_request", version)
}

func failure(code string, version int64) Response {
	return Response{OK: false, Error: code, AppliedVersion: version, Health: "unhealthy"}
}

func (s *Service) status(current *state) Response {
	if current == nil {
		if _, err := s.runtimeHash(); err != nil {
			return failure("caddy_unavailable", 0)
		}
		return Response{OK: true, Health: "unmanaged"}
	}
	data, err := os.ReadFile(s.config.CaddyConfig)
	if err != nil || digest(data) != current.ConfigHash {
		return failure("config_drift", current.Version)
	}
	hash, err := s.runtimeHash()
	if err != nil {
		return failure("caddy_unavailable", current.Version)
	}
	if hash != current.RuntimeHash {
		return failure("runtime_drift", current.Version)
	}
	return Response{OK: true, Health: "healthy", AppliedVersion: current.Version}
}

func (s *Service) apply(request Request, current *state) Response {
	version := int64(0)
	if current != nil {
		version = current.Version
	}
	template, err := os.ReadFile(s.config.TemplateFile)
	if err != nil {
		return failure("invalid_template", version)
	}
	users := sortedUsers(request.Users)
	userJSON, _ := json.Marshal(users)
	requestHash := digest(append(append(userJSON, '\n'), template...))
	if current != nil && request.Version <= current.Version {
		if request.Version == current.Version && requestHash == current.RequestHash {
			return s.status(current)
		}
		return failure("version_conflict", version)
	}
	oldConfig, err := os.ReadFile(s.config.CaddyConfig)
	if err != nil {
		return failure("config_unavailable", version)
	}
	if current != nil && digest(oldConfig) != current.ConfigHash {
		return failure("config_drift", version)
	}
	oldHash, err := s.adapt(s.config.CaddyConfig)
	if err != nil {
		return failure("config_invalid", version)
	}
	runtimeHash, err := s.runtimeHash()
	if err != nil {
		return failure("caddy_unavailable", version)
	}
	if oldHash != runtimeHash {
		return failure("runtime_drift", version)
	}
	candidate, err := renderTemplate(string(template), users)
	if err != nil {
		return failure("invalid_template", version)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.config.CaddyConfig), ".naive-candidate-*")
	if err != nil {
		return failure("config_unavailable", version)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(candidate); err != nil {
		temp.Close()
		return failure("config_unavailable", version)
	}
	if err = temp.Close(); err != nil {
		return failure("config_unavailable", version)
	}
	if _, err = s.command(s.config.CaddyBinary, "validate", "--config", tempName, "--adapter", "caddyfile"); err != nil {
		return failure("validation_failed", version)
	}
	candidateRuntimeHash, err := s.adapt(tempName)
	if err != nil {
		return failure("validation_failed", version)
	}
	tx := transaction{PreviousConfig: oldConfig, PreviousState: current, RuntimeHash: oldHash}
	journal, _ := json.Marshal(tx)
	if err = atomicWrite(s.statePath("transaction.json"), journal, 0600, nil); err != nil {
		return failure("state_unavailable", version)
	}
	if err = s.install(candidate); err != nil {
		return s.failedApply("install_failed", version)
	}
	if err = s.reload(); err != nil {
		return s.failedApply("reload_failed", version)
	}
	if err = s.confirm(candidateRuntimeHash); err != nil {
		return s.failedApply("runtime_mismatch", version)
	}
	updated := state{Version: request.Version, RequestHash: requestHash, ConfigHash: digest(candidate), RuntimeHash: candidateRuntimeHash}
	data, _ := json.Marshal(updated)
	if err = atomicWrite(s.statePath("state.json"), data, 0600, nil); err != nil {
		return s.failedApply("state_unavailable", version)
	}
	if err = removeDurable(s.statePath("transaction.json")); err != nil {
		return s.failedApply("state_unavailable", version)
	}
	return Response{OK: true, Health: "healthy", AppliedVersion: request.Version}
}

func (s *Service) failedApply(code string, version int64) Response {
	if err := s.recover(); err != nil {
		return failure("rollback_failed", version)
	}
	return failure(code, version)
}

// A durable transaction exists before any change to the running configuration.
// Until its removal, an interrupted application always restores the prior state.
func (s *Service) recover() error {
	data, err := os.ReadFile(s.statePath("transaction.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var tx transaction
	if err = decodeStrict(data, &tx); err != nil || len(tx.PreviousConfig) == 0 || tx.RuntimeHash == "" {
		return errors.New("invalid_transaction")
	}
	if err = s.install(tx.PreviousConfig); err != nil {
		return err
	}
	if err = s.reload(); err != nil {
		return err
	}
	if err = s.confirm(tx.RuntimeHash); err != nil {
		return err
	}
	if tx.PreviousState == nil {
		if err = removeDurable(s.statePath("state.json")); err != nil {
			return err
		}
	} else {
		previous, _ := json.Marshal(tx.PreviousState)
		if err = atomicWrite(s.statePath("state.json"), previous, 0600, nil); err != nil {
			return err
		}
	}
	return removeDurable(s.statePath("transaction.json"))
}

func (s *Service) statePath(name string) string { return filepath.Join(s.config.StateDir, name) }

func (s *Service) readState() (*state, error) {
	data, err := os.ReadFile(s.statePath("state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value state
	if err = decodeStrict(data, &value); err != nil || value.Version <= 0 || value.RequestHash == "" || value.ConfigHash == "" || value.RuntimeHash == "" {
		return nil, errors.New("invalid_state")
	}
	return &value, nil
}

func (s *Service) command(binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = filepath.Dir(s.config.CaddyConfig)
	// An SSH request cannot inject environment variables into Caddy evaluation.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	var stdout limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return stdout.Bytes(), err
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p)+b.Len() > 8<<20 {
		return 0, errors.New("command_output_limit")
	}
	return b.Buffer.Write(p)
}

func (s *Service) reload() error {
	_, err := s.command(s.config.CaddyBinary, "reload", "--config", s.config.CaddyConfig, "--adapter", "caddyfile", "--address", s.config.AdminAddress)
	return err
}

func (s *Service) adapt(path string) (string, error) {
	data, err := s.command(s.config.CaddyBinary, "adapt", "--config", path, "--adapter", "caddyfile")
	if err != nil {
		return "", err
	}
	return jsonDigest(data)
}

func (s *Service) runtimeHash() (string, error) {
	client := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected_redirect") },
	}
	response, err := client.Get("http://" + s.config.AdminAddress + "/config/")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("admin_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return "", errors.New("invalid_runtime_config")
	}
	return jsonDigest(data)
}

func (s *Service) confirm(expected string) error {
	// Reload's success means synchronous Caddy config provisioning completed.
	actual, err := s.runtimeHash()
	if err != nil || actual != expected {
		return errors.New("runtime_mismatch")
	}
	return nil
}

func (s *Service) install(data []byte) error {
	info, err := os.Stat(s.config.CaddyConfig)
	if err != nil {
		return err
	}
	return atomicWrite(s.config.CaddyConfig, data, info.Mode().Perm(), info.Sys().(*syscall.Stat_t))
}

func atomicWrite(path string, data []byte, mode os.FileMode, owner *syscall.Stat_t) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".naive-write-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	if err = file.Chmod(mode); err != nil {
		return err
	}
	if owner != nil {
		if err = file.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
			return err
		}
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func removeDurable(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func jsonDigest(data []byte) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", errors.New("invalid_json")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digest(canonical), nil
}
