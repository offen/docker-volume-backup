#!/bin/sh
# This test refers to https://github.com/offen/docker-volume-backup/issues/71

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

tmp_dir=$(mktemp -d)
tar --same-owner -xvf ./local/backup.tar.gz -C $tmp_dir

find $tmp_dir/backup/postgres > /dev/null
pass "Backup contains files at expected location"

for file in $(find $tmp_dir/backup/postgres); do
  if [ "$(stat -c '%u:%g' $file)" != "70:70" ]; then
    fail "Unexpected file ownership for $file: $(stat -c '%u:%g' $file)"
  fi
done
pass "All files and directories in backup preserved their ownership."

docker compose down --volumes
