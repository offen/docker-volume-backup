#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

export KEY_DIR=$(mktemp -d)

export PASSPHRASE="test"

gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: offen
Name-Email: docker-volume-backup@local
Expire-Date: 0
Passphrase: $PASSPHRASE
%commit
EOF

gpg --export --armor --batch --yes --pinentry-mode loopback --passphrase $PASSPHRASE --output $KEY_DIR/public_key.asc

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

expect_running_containers "2"

TMP_DIR=$(mktemp -d)

echo "test" | gpg -d --pinentry-mode loopback --yes --passphrase-fd 0 "$LOCAL_DIR/test.tar.gz.gpg" > "$LOCAL_DIR/decrypted.tar.gz"

tar -xf "$LOCAL_DIR/decrypted.tar.gz" -C $TMP_DIR

if [ ! -f $TMP_DIR/backup/app_data/offen.db ]; then
  fail "Could not find expected file in untared archive."
fi
rm "$LOCAL_DIR/decrypted.tar.gz"

pass "Found relevant files in decrypted and untared local backup."

if [ ! -L "$LOCAL_DIR/test-latest.tar.gz.gpg" ]; then
  fail "Could not find local symlink to latest encrypted backup."
fi
