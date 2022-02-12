#!/bin/sh

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d
sleep 5

GOTIFY_TOKEN=$(curl -sSLX POST -H 'Content-Type: application/json' -d '{"name":"test"}' http://admin:custom@localhost:8080/application | jq -r '.token')

docker-compose down

GOTIFY_TOKEN=$GOTIFY_TOKEN docker-compose up -d

echo "[TEST:INFO] Set up Gotify appliaction using token $GOTIFY_TOKEN"

docker-compose exec backup backup

tar -xf ./local/test.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db
echo "[TEST:PASS] Found relevant files in untared local backup."

if [ "$(docker-compose ps -q | wc -l)" != "3" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

MESSAGE=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages[0]')

if [[ "$MESSAGE" != *"Successful test run, yay!"* ]]; then
  echo "[TEST:FAIL] Expected custom title to be used in notification, instead seen:"
  echo $MESSAGE
  exit 1
fi

if [[ "$MESSAGE" != *"Backing up test.tar.gz succeeded."* ]]; then
  echo "[TEST:FAIL] Expected custom title to be used in notification, instead seen:"
  echo $MESSAGE
  exit 1
fi

echo "[TEST:PASS] Custom notifications were used as expected."

docker-compose down --volumes
