#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

expect_running_containers "2"

tmp_dir=$(mktemp -d)

echo "1234#\$ecret" | gpg -d --pinentry-mode loopback --yes --passphrase-fd 0 ./local/test.tar.gz.gpg > ./local/decrypted.tar.gz
tar -xf ./local/decrypted.tar.gz -C $tmp_dir
if [ ! -f $tmp_dir/backup/app_data/offen.db ]; then
  fail "Could not find expected file in untared archive."
fi
rm ./local/decrypted.tar.gz

pass "Found relevant files in decrypted and untared local backup."

if [ ! -L ./local/test-latest.tar.gz.gpg ]; then
  fail "Could not find local symlink to latest encrypted backup."
fi

docker compose down --volumes --timeout 3
