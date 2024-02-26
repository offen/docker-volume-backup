#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d

sleep 20

expect_running_containers "3"

docker compose exec backup backup

sleep 5

if [ ! -f $LOCAL_DIR/test.tar.gz ]; then
  fail "Archive was not created"
fi
pass "Found expected file."

expect_running_containers "3"
