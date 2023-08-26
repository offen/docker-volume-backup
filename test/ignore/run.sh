#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5
docker compose exec backup backup

TMP_DIR=$(mktemp -d)
tar --same-owner -xvf "$LOCAL_DIR/test.tar.gz" -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/backup/data/me.txt" ]; then
  fail "Expected file was not found."
fi
pass "Expected file was found."

if [ -f "$TMP_DIR/backup/data/skip.me" ]; then
  fail "Ignored file was found."
fi
pass "Ignored file was not found."
