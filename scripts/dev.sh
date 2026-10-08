#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .test/ssh
if [ ! -f .test/admin_password ]; then
  (umask 077; openssl rand -base64 32 > .test/admin_password)
fi
export DASHBOARD_ADMIN_PASSWORD_FILE="$PWD/.test/admin_password"
export DASHBOARD_SSH_KEY_DIR="$PWD/.test/ssh"
export DASHBOARD_DB="$PWD/.test/dashboard.db"
export DASHBOARD_ADDR=127.0.0.1:8080
export DASHBOARD_COOKIE_SECURE=false
exec go run ./cmd/dashboard
