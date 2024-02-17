#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

docker compose logs backup

# conf.d is used to confirm /etc files are also accessible for non-root users
docker compose exec backup /bin/sh -c 'set -a; source /etc/dockervolumebackup/conf.d/01conf.env; set +a && backup'

sleep 5

expect_running_containers "3"

if [ ! -f "$LOCAL_DIR/backup/test.tar.gz" ]; then
  fail "Could not find archive."
fi
pass "Archive was created."

