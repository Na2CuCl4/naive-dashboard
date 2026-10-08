package node

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Config struct {
	CaddyBinary    string   `json:"caddy_binary"`
	CaddyConfig    string   `json:"caddy_config"`
	TemplateFile   string   `json:"template_file"`
	StateDir       string   `json:"state_dir"`
	UsageDir       string   `json:"usage_dir"`
	AdminAddress   string   `json:"admin_address"`
	RestartCommand []string `json:"restart_command,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	if err := trustedPath(path, false); err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 32768 {
		return cfg, errors.New("invalid_node_config")
	}
	if err = decodeStrict(data, &cfg); err != nil {
		return cfg, err
	}
	if err = cfg.validate(); err != nil {
		return cfg, err
	}
	for _, path := range []string{cfg.CaddyBinary, cfg.CaddyConfig, cfg.TemplateFile, cfg.StateDir} {
		if err = trustedPath(path, path == cfg.StateDir); err != nil {
			return cfg, err
		}
	}
	if len(cfg.RestartCommand) > 0 {
		if err = trustedPath(cfg.RestartCommand[0], false); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	for _, path := range []string{cfg.CaddyBinary, cfg.CaddyConfig, cfg.TemplateFile, cfg.StateDir, cfg.UsageDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("invalid_node_config")
		}
	}
	if cfg.CaddyConfig == cfg.TemplateFile || cfg.StateDir == cfg.UsageDir {
		return errors.New("invalid_node_config")
	}
	host, port, err := net.SplitHostPort(cfg.AdminAddress)
	if err != nil {
		return errors.New("invalid_node_config")
	}
	ip := net.ParseIP(host)
	number, err := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || err != nil || number < 1 || number > 65535 {
		return errors.New("invalid_node_config")
	}
	if len(cfg.RestartCommand) > 20 || (len(cfg.RestartCommand) > 0 && !filepath.IsAbs(cfg.RestartCommand[0])) {
		return errors.New("invalid_node_config")
	}
	for _, arg := range cfg.RestartCommand {
		if len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return errors.New("invalid_node_config")
		}
	}
	return nil
}

// Production runs as root through a fixed sudoers entry. Every trusted path and
// ancestor must be controlled by root. Non-root direct invocation is useful for
// isolated local tests and accepts paths controlled by that same uid or root.
func trustedPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("untrusted_path")
	}
	first := true
	for {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("untrusted_path")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) {
			return errors.New("untrusted_path")
		}
		// Root-owned sticky directories (e.g. /tmp) cannot unlink others' files.
		stickyRoot := !first && stat.Uid == 0 && info.IsDir() && info.Mode()&os.ModeSticky != 0
		if info.Mode().Perm()&0022 != 0 && !stickyRoot {
			return errors.New("untrusted_path")
		}
		if first && ((directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular())) {
			return errors.New("untrusted_path")
		}
		if path == "/" {
			return nil
		}
		first = false
		path = filepath.Dir(path)
	}
}
