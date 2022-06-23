#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh

mkdir -p local

docker-compose up -d
sleep 5
docker-compose exec backup backup

docker-compose down --volumes

out=$(mktemp -d)
sudo tar --same-owner -xvf ./local/test.tar.gz -C "$out"

if [ ! -f "$out/backup/data/me.txt" ]; then
  fail "Expected file was not found."
fi
pass "Expected file was found."

if [ -f "$out/backup/data/skip.me" ]; then
  fail "Ignored file was found."
fi
pass "Ignored file was not found."
