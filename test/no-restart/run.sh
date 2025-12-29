#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull --wait

docker compose exec backup backup

sleep 5

expect_running_containers "1"

tmp_dir=$(mktemp -d)
tar -xvf "$LOCAL_DIR/test.tar.gz" -C $tmp_dir
if [ ! -f "$tmp_dir/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi
