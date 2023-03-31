#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

docker compose up -d

user_name=testuser
docker exec user-alpine-1 adduser --disabled-password "$user_name"

docker compose exec backup backup
sudo cp -r $(docker volume inspect --format='{{ .Mountpoint }}' user_archive) ./local

tar -xvf ./local/test.tar.gz
if [ ! -f ./backup/data/whoami.txt ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

tar -xvf ./local/test.tar.gz
if [ "$(cat ./backup/data/whoami.txt)" != "$user_name" ]; then
  fail "Could not find expected user name."
fi
pass "Found expected user."

docker compose down --volumes
sudo rm -rf ./local

