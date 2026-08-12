#!/bin/sh
# Prepares one test server and hands the container over to sshd.
#
# REMOTE_CONTROL decides whether unbound-control is usable, which is how the
# stack ends up with a server that can only be refreshed by signal or restart.
set -eu

REMOTE_CONTROL=${REMOTE_CONTROL:-yes}
PUBLIC_KEY=${PUBLIC_KEY:-/keys/authorized_key.pub}
CONFIG_FILE=/etc/unbound/unbound.conf

if [ ! -f "$PUBLIC_KEY" ]; then
    echo "Ortak anahtar bulunamadı: $PUBLIC_KEY" >&2
    echo "Önce 'make docker-keys' çalıştırın." >&2
    exit 1
fi

install -m 0600 -o user01 -g user01 "$PUBLIC_KEY" /home/user01/.ssh/authorized_keys

# Host keys are generated per container rather than baked into the image, so
# no private key material is ever committed.
ssh-keygen -A >/dev/null

if [ "$REMOTE_CONTROL" = "yes" ]; then
    unbound-control-setup -d /etc/unbound >/dev/null 2>&1

    cat > /etc/unbound/remote-control.conf <<'EOF'
remote-control:
    control-enable: yes
    control-interface: 127.0.0.1
    control-port: 8953
    server-key-file: "/etc/unbound/unbound_server.key"
    server-cert-file: "/etc/unbound/unbound_server.pem"
    control-key-file: "/etc/unbound/unbound_control.key"
    control-cert-file: "/etc/unbound/unbound_control.pem"
EOF
else
    cat > /etc/unbound/remote-control.conf <<'EOF'
remote-control:
    control-enable: no
EOF
fi

# Fail here rather than after sshd is up, so a broken fixture is obvious.
unbound-checkconf "$CONFIG_FILE" >/dev/null || {
    echo "Test sunucusunun config'i geçersiz:" >&2
    unbound-checkconf "$CONFIG_FILE" >&2
    exit 1
}

systemctl start unbound

echo "hazır: remote-control=$REMOTE_CONTROL"

exec /usr/sbin/sshd -D -e
