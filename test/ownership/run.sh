#!/bin/sh
# This test refers to https://github.com/offen/docker-volume-backup/issues/71

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

TMP_DIR=$(mktemp -d)
tar --same-owner -xvf "$LOCAL_DIR/backup.tar.gz" -C $TMP_DIR

find $TMP_DIR/backup/postgres > /dev/null
pass "Backup contains files at expected location"

for file in $(find $TMP_DIR/backup/postgres); do
  if [ "$(stat -c '%u:%g' $file)" != "70:70" ]; then
    fail "Unexpected file ownership for $file: $(stat -c '%u:%g' $file)"
  fi
done
pass "All files and directories in backup preserved their ownership."
