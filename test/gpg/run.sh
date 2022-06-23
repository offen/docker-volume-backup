#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh

mkdir -p local

docker-compose up -d
sleep 5

docker-compose exec backup backup

expect_running_containers "2"

echo 1234secret | gpg -d --pinentry-mode loopback --yes --passphrase-fd 0 ./local/test.tar.gz.gpg > ./local/decrypted.tar.gz
tar -xf ./local/decrypted.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db
rm ./local/decrypted.tar.gz
test -L /tmp/backup/app_data/db.link

pass "Found relevant files in decrypted and untared local backup."

docker-compose down --volumes
