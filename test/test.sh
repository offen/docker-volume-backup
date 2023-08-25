#!/bin/sh

set -e

docker build -t offen/docker-volume-backup-test-sandbox:latest .

if [ "$(docker ps -f "name=docker_volume_backup_test_sandbox" --format '{{.Names}}')" != "docker_volume_backup_test_sandbox" ]; then
  docker run --name "$name" --detach \
    --privileged \
    --name docker_volume_backup_test_sandbox \
    -v $(dirname $(pwd)):/code \
    offen/docker-volume-backup-test-sandbox:latest
fi

sleep 5

docker exec docker_volume_backup_test_sandbox /bin/sh -c 'docker build -t offen/docker-volume-backup:canary /code'
docker exec docker_volume_backup_test_sandbox /bin/sh -c '/code/test/run.sh'
