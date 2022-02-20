#!/bin/sh
# This test refers to https://github.com/offen/docker-volume-backup/issues/71

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d
sleep 5

docker-compose exec backup backup

sudo tar --same-owner -xvf ./local/backup.tar.gz -C /tmp

sudo find /tmp/backup/postgres > /dev/null
echo "[TEST:PASS] Backup contains files at expected location"

for file in $(sudo find /tmp/backup/postgres); do
  if [ "$(sudo stat -c '%u:%g' $file)" != "70:70" ]; then
    echo "[TEST:FAIL] Unexpected file ownership for $file: $(sudo stat -c '%u:%g' $file)"
    exit 1
  fi
done
echo "[TEST:PASS] All files and directories in backup preserved their ownership."

docker-compose down --volumes
