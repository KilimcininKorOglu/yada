#!/usr/bin/env bash
# Generates the throwaway ssh key the test containers trust.
#
# The key is created on the machine that runs the tests and never enters the
# repository, so no private key material is ever committed.
set -euo pipefail

cd "$(dirname "$0")"

KEY=keys/id_test

if [ -f "$KEY" ]; then
    exit 0
fi

mkdir -p keys

ssh-keygen -t ed25519 -N '' -C 'yada test fixture' -f "$KEY" >/dev/null

chmod 0600 "$KEY"

echo "Test anahtarı üretildi: docker/$KEY"
