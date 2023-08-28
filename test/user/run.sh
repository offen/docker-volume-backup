#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)
export TMP_DIR=$(mktemp -d)

echo "LOCAL_DIR $LOCAL_DIR"
echo "TMP_DIR $TMP_DIR"

docker compose up -d --quiet-pull
user_name=testuser
docker exec user-alpine-1 adduser --disabled-password "$user_name"

docker compose exec backup backup

tar -xvf "$LOCAL_DIR/test.tar.gz" -C "$TMP_DIR"
if [ ! -f "$TMP_DIR/backup/data/whoami.txt" ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

if [ "$(cat $TMP_DIR/backup/data/whoami.txt)" != "$user_name" ]; then
  fail "Could not find expected user name."
fi
pass "Found expected user."
