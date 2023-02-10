#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d
sleep 10

docker compose exec backup backup

if [ ! -f "./local/backup.tar.gz" ]; then
  fail "Could not find expected backup file."
fi

tmp_dir=$(mktemp -d)
tar -xvf ./local/backup.tar.gz -C $tmp_dir
if [ ! -f "$tmp_dir/backup/order/test.txt" ]; then
  fail "Could not find expected file in untared archive."
fi

docker compose down --volumes
