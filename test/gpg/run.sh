#!/bin/sh

set -e

cd "$(dirname "$0")"

mkdir -p local

docker-compose up -d
sleep 5

docker-compose exec backup backup

sleep 5
if [ "$(docker-compose ps -q | wc -l)" != "2" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi
echo "[TEST:PASS] All containers running post backup."

echo 1234secret | gpg -d --pinentry-mode loopback --yes --passphrase-fd 0 ./local/test.tar.gz.gpg > ./local/decrypted.tar.gz
tar -xf ./local/decrypted.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db
rm ./local/decrypted.tar.gz
test -L /tmp/backup/app_data/db.link

echo "[TEST:PASS] Found relevant files in decrypted and untared local backup."

docker-compose down --volumes
