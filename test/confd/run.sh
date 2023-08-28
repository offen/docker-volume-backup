#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d --quiet-pull

# sleep until a backup is guaranteed to have happened on the 1 minute schedule
sleep 100

docker compose down --volumes --timeout 3

if [ ! -f ./local/conf.tar.gz ]; then
  fail "Config from file was not used."
fi
pass "Config from file was used."

if [ ! -f ./local/other.tar.gz ]; then
  fail "Run on same schedule did not succeed."
fi
pass "Run on same schedule succeeded."

if [ -f ./local/never.tar.gz ]; then
  fail "Unexpected file was found."
fi
pass "Unexpected cron did not run."
