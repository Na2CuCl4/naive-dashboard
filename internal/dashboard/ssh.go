package dashboard

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Transport interface {
	Call(context.Context, Node, RPCRequest) (RPCResponse, error)
}
type SSHTransport struct{ KeyDir string }
type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("response too large")
	}
	return b.Buffer.Write(p)
}
func (t SSHTransport) Call(ctx context.Context, n Node, req RPCRequest) (RPCResponse, error) {
	var out RPCResponse
	if !safeRef.MatchString(n.KeyRef) || strings.Contains(n.KeyRef, "..") {
		return out, errors.New("invalid_key")
	}
	root, e := filepath.EvalSymlinks(t.KeyDir)
	if e != nil {
		return out, errors.New("invalid_key")
	}
	path, e := filepath.EvalSymlinks(filepath.Join(root, n.KeyRef))
	if e != nil || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return out, errors.New("invalid_key")
	}
	f, e := os.Open(path)
	if e != nil {
		return out, errors.New("invalid_key")
	}
	data, e := io.ReadAll(io.LimitReader(f, 65537))
	f.Close()
	if e != nil || len(data) > 65536 {
		return out, errors.New("invalid_key")
	}
	signer, e := ssh.ParsePrivateKey(data)
	if e != nil {
		return out, errors.New("invalid_key")
	}
	mismatch := false
	conf := &ssh.ClientConfig{User: n.SSHUser, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(key)), []byte(n.HostFingerprint)) != 1 {
			mismatch = true
			return errors.New("host_key_mismatch")
		}
		return nil
	}}
	address := net.JoinHostPort(n.Host, fmt.Sprint(n.SSHPort))
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, e := d.DialContext(ctx, "tcp", address)
	if e != nil {
		return out, errors.New("node_unreachable")
	}
	defer conn.Close()
	deadline := time.Now().Add(45 * time.Second)
	if v, ok := ctx.Deadline(); ok && v.Before(deadline) {
		deadline = v
	}
	conn.SetDeadline(deadline)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	c, ch, reqs, e := ssh.NewClientConn(conn, address, conf)
	if e != nil {
		if mismatch {
			return out, errors.New("host_key_mismatch")
		}
		return out, errors.New("node_unreachable")
	}
	client := ssh.NewClient(c, ch, reqs)
	defer client.Close()
	session, e := client.NewSession()
	if e != nil {
		return out, errors.New("node_unreachable")
	}
	defer session.Close()
	buf := &cappedBuffer{max: 8 << 20}
	session.Stdout = buf
	session.Stderr = io.Discard
	session.Stdin = strings.NewReader(jsonText(req) + "\n")
	runErr := session.Run("naive-node rpc")
	// A node can return a structured error with a non-zero process status.
	if e = json.Unmarshal(buf.Bytes(), &out); e != nil {
		if runErr != nil {
			return out, errors.New("node_unreachable")
		}
		return out, errors.New("node_error")
	}
	if !out.OK {
		if safeID.MatchString(out.Error) {
			return out, errors.New(out.Error)
		}
		return out, errors.New("node_error")
	}
	if runErr != nil {
		return out, errors.New("node_error")
	}
	return out, nil
}
