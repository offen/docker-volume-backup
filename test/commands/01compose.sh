#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)
export TMP_DIR=$(mktemp -d)

docker compose up -d --quiet-pull --wait

docker compose exec backup backup

tar -xvf "$LOCAL_DIR/test.tar.gz" -C $TMP_DIR
if [ ! -f "$TMP_DIR/backup/data/dump.sql" ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

if [ -f "$TMP_DIR/backup/data/not-relevant.txt" ]; then
  fail "Command ran for container with other label."
fi
pass "Command did not run for container with other label."

if [ -f "$TMP_DIR/backup/data/post.txt" ]; then
  fail "File created in post command was present in backup."
fi
pass "Did not find unexpected file."

docker compose down --volumes
