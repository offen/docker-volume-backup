#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename "$(pwd)")

export LOCAL_DIR="$(mktemp -d)"

docker compose up -d --quiet-pull --wait

docker compose exec backup backup

expect_running_containers "2"

TMP_DIR=$(mktemp -d)

# complex usage of expect(1) due to age not have a way to programmatically
# provide the passphrase
expect -i <<EOL
spawn age --decrypt -o "$LOCAL_DIR/decrypted.tar.gz" "$LOCAL_DIR/test.tar.gz.age"
expect -exact "Enter passphrase: "
send -- "Dance.0Tonight.Go.Typical\r"
sleep 1
EOL
tar -xf "$LOCAL_DIR/decrypted.tar.gz" -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi
rm -vf "$LOCAL_DIR/decrypted.tar.gz"

pass "Found relevant files in decrypted and untared local backup."

if [ ! -L "$LOCAL_DIR/test-latest.tar.gz.age" ]; then
  fail "Could not find local symlink to latest encrypted backup."
fi
