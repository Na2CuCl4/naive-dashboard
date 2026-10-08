package main

import (
	"context"
	"github.com/Na2CuCl4/naive-dashboard/internal/dashboard"
	"github.com/Na2CuCl4/naive-dashboard/web"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	syscall.Umask(0077)
	password, e := dashboard.ReadPassword(env("DASHBOARD_ADMIN_PASSWORD_FILE", "/run/secrets/admin_password"))
	if e != nil {
		log.Fatal("cannot read administrator password file")
	}
	s, e := dashboard.New(dashboard.Options{DBPath: env("DASHBOARD_DB", "/data/dashboard.db"), Password: password, KeyDir: env("DASHBOARD_SSH_KEY_DIR", "/run/secrets/ssh"), SecureCookie: env("DASHBOARD_COOKIE_SECURE", "true") != "false", Assets: web.FS})
	if e != nil {
		log.Fatal(e)
	}
	defer s.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); s.RunWorker(ctx) }()
	h := &http.Server{Addr: env("DASHBOARD_ADDR", ":8080"), Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dashboard.Shutdown(c, h)
	}()
	log.Printf("dashboard listening on %s", h.Addr)
	if e = h.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
	<-workerDone
}
