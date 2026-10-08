#!/bin/sh
set -eu
mkdir -p /keys /run/sshd
if [ ! -f /keys/host ]; then ssh-keygen -q -t ed25519 -N '' -f /keys/host; fi
printf 'restrict,command="/usr/local/bin/naive-node rpc" %s\n' "$(cat /control.pub)" > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
sed 's/# NAIVE_USERS/basic_auth bootstrap_deny never_distributed_credential/' /etc/naive-node/Caddyfile.template > /etc/naive-node/Caddyfile
python3 /usr/local/bin/echo.py &
/usr/sbin/sshd -D -e -h /keys/host -o PermitRootLogin=prohibit-password -o PasswordAuthentication=no -o AllowTcpForwarding=no -o X11Forwarding=no -o PermitTTY=no &
exec /usr/local/bin/caddy run --config /etc/naive-node/Caddyfile --adapter caddyfile
