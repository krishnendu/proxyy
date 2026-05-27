#!/usr/bin/env bash
# proxyy server installer. Run as root on a fresh Ubuntu VM after dropping the
# `proxyy-server` binary in your home directory.
#
# Usage:
#   sudo BINARY=~/proxyy-server bash deploy/install.sh
#
# Idempotent: re-running upgrades the binary and reloads systemd without
# touching the existing env file or cert cache.

set -euo pipefail

BINARY="${BINARY:-./proxyy-server}"

if [[ $EUID -ne 0 ]]; then
  echo "must run as root (try: sudo $0)"; exit 1
fi
if [[ ! -x "$BINARY" ]]; then
  echo "binary not found or not executable: $BINARY"; exit 1
fi

# Dedicated unprivileged system user (no shell, no home).
if ! id -u proxyy >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin proxyy
  echo "created user 'proxyy'"
fi

install -m 0755 -o root -g root "$BINARY" /usr/local/bin/proxyy-server
echo "installed /usr/local/bin/proxyy-server"

install -d -m 0750 -o proxyy -g proxyy /etc/proxyy
if [[ ! -f /etc/proxyy/proxyy.env ]]; then
  install -m 0600 -o proxyy -g proxyy deploy/proxyy.env.example /etc/proxyy/proxyy.env
  echo
  echo ">>> /etc/proxyy/proxyy.env was created from the example."
  echo ">>> EDIT IT NOW (set TUNNEL_AUTH_TOKEN and TUNNEL_ACME_EMAIL) before starting the service."
  echo
fi

install -m 0644 deploy/proxyy.service /etc/systemd/system/proxyy.service

systemctl daemon-reload
echo "systemd reloaded"

echo
echo "Next steps:"
echo "  1. sudo \$EDITOR /etc/proxyy/proxyy.env       # set the auth token + email"
echo "  2. sudo systemctl enable --now proxyy        # start now and on boot"
echo "  3. sudo systemctl status proxyy              # confirm it's running"
echo "  4. journalctl -u proxyy -f                   # tail the logs"
