#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
command -v go >/dev/null
proxy_binary=${NAIVE_CADDY_BINARY:-../forwardproxy/build/naive-caddy}
[ -x "$proxy_binary" ] || { echo 'Build the fixed forwardproxy development binary first.' >&2; exit 1; }
mkdir -p .test/integration/ssh
if [ -e .test/integration/admin_password ]; then
  echo 'Existing test fixture: run scripts/integration/cleanup.sh before starting a fresh test.' >&2
  exit 1
fi
cp scripts/integration/{node.Dockerfile,node-entrypoint.sh,echo.py,node-config.json,Caddyfile.template,compose.yaml} .test/integration/
cp "$proxy_binary" .test/integration/naive-caddy
CGO_ENABLED=0 GOMAXPROCS=2 GOFLAGS=-p=1 go build -o .test/integration/naive-node ./cmd/naive-node
(umask 077; openssl rand -base64 32 > .test/integration/admin_password; ssh-keygen -q -t ed25519 -N '' -f .test/integration/ssh/control)
cp .test/integration/ssh/control.pub .test/integration/control.pub
# Container UID can read only these test secrets. These are isolated disposable keys.
sudo -n chown 10001:10001 .test/integration/ssh/control
sudo -n chown 10001:"$(id -g)" .test/integration/admin_password
sudo -n chmod 440 .test/integration/admin_password
printf "ssh\nadmin_password\ncontrol.pub\ncompose.yaml\n" > .test/integration/.dockerignore
sudo -n docker build -t naive-dashboard:local .
sudo -n docker build -t naive-dashboard-test-node:local -f .test/integration/node.Dockerfile .test/integration
sudo -n docker compose -p naive-mvp-test -f .test/integration/compose.yaml up -d
python3 scripts/integration/check.py
