#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

export TEST_VERSION="${TEST_VERSION:-canary}-with-rsync"

docker build . -t offen/docker-volume-backup:$TEST_VERSION

docker-compose up -d
sleep 5

docker-compose exec backup backup

sleep 5

expect_running_containers "2"

tmp_dir=$(mktemp -d)
tar -xvf ./local/test.tar.gz -C $tmp_dir
if [ ! -f "$tmp_dir/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi

docker-compose down --volumes
