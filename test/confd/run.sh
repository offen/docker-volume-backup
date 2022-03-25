#!/bin/sh

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d

# sleep until a backup is guaranteed to have happened on the 1 minute schedule
sleep 100

docker-compose down --volumes

if [ ! -f ./local/conf.tar.gz ]; then
  echo "[TEST:FAIL] Config from file was not used."
  exit 1
fi
echo "[TEST:PASS] Config from file was used."

if [ ! -f ./local/other.tar.gz ]; then
  echo "[TEST:FAIL] Run on same schedule did not succeed."
  exit 1
fi
echo "[TEST:PASS] Run on same schedule succeeded."

if [ -f ./local/never.tar.gz ]; then
  echo "[TEST:FAIL] Unexpected file was found."
  exit 1
fi
echo "[TEST:PASS] Unexpected cron did not run."
