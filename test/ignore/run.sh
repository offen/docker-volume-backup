#!/bin/sh

set -e

cd $(dirname $0)
mkdir -p local

docker-compose up -d
sleep 5
docker-compose exec backup backup

out=$(mktemp -d)
sudo tar --same-owner -xvf ./local/test.tar.gz -C "$out"

if [ ! -f "$out/backup/data/me.txt" ]; then
  echo "[TEST:FAIL] Expected file was not found."
  exit 1
fi
echo "[TEST:PASS] Expected file was found."

if [ -f "$out/backup/data/skip.me" ]; then
  echo "[TEST:FAIL] Ignored file was found."
  exit 1
fi
echo "[TEST:PASS] Ignored file was not found."
