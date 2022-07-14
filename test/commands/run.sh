#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

docker-compose up -d
sleep 30 # mariadb likes to take a bit before responding

docker-compose exec backup backup
sudo cp -r $(docker volume inspect --format='{{ .Mountpoint }}' commands_archive) ./local

tar -xvf ./local/test.tar.gz
if [ ! -f ./backup/data/dump.sql ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

if [ -f ./backup/data/not-relevant.txt ]; then
  fail "Command ran for container with other label."
fi
pass "Command did not run for container with other label."

if [ -f ./backup/data/post.txt ]; then
  fail "File created in post command was present in backup."
fi
pass "Did not find unexpected file."

docker-compose down --volumes
sudo rm -rf ./local


info "Running commands test in swarm mode next."

docker swarm init

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  info "Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

docker exec $(docker ps -q -f name=backup) backup

sudo cp -r $(docker volume inspect --format='{{ .Mountpoint }}' test_stack_archive) ./local

tar -xvf ./local/test.tar.gz
if [ ! -f ./backup/data/dump.sql ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

if [ -f ./backup/data/post.txt ]; then
  fail "File created in post command was present in backup."
fi
pass "Did not find unexpected file."

docker stack rm test_stack
docker swarm leave --force
