#!/usr/bin/env bash
# Blocks until every test server answers over ssh.
#
# docker compose returns as soon as the containers start, which is before sshd
# is listening. Without this a test would fail on a race rather than on a bug.
set -euo pipefail

cd "$(dirname "$0")/.."

PORTS=(8340 8342 8344)
DEADLINE=$((SECONDS + 90))

for port in "${PORTS[@]}"; do
    until ssh -p "$port" \
        -o BatchMode=yes \
        -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR \
        -o ConnectTimeout=3 \
        -o IdentitiesOnly=yes \
        -i docker/keys/id_test \
        user01@127.0.0.1 'sudo systemctl is-active unbound' >/dev/null 2>&1; do

        if [ "$SECONDS" -ge "$DEADLINE" ]; then
            echo "Sunucu $port zamanında hazır olmadı." >&2
            docker compose -f docker/docker-compose.yml logs --tail 40 >&2
            exit 1
        fi

        sleep 2
    done

    echo "hazır: 127.0.0.1:$port"
done
