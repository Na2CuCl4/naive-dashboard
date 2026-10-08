#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
if [ -f .test/integration/compose.yaml ]; then
  sudo -n docker compose -p naive-mvp-test -f .test/integration/compose.yaml down -v
fi
# Delete only this generated fixture, never the regular Dashboard or host Caddy.
sudo -n rm -rf -- .test/integration
