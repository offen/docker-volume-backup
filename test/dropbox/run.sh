#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

docker compose up -d
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

dvb_logs=$(docker logs backup-1 2>&1)
if $dvb_logs | grep "ERROR"
then
  fail "Backup failed, errors reported: $dvb_logs"
else
  pass "Backup succeeded, no errors reported."
fi

dbx_logs=$(docker logs openapi_mock-1 2>&1)
if $dbx_logs | grep "ERROR"
then
  fail "Backup failed, errors reported: $dvb_logs"
else
  pass "Backup succeeded, no errors reported."
fi

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

docker compose exec backup backup

dvb_logs=$(docker logs backup-1 2>&1)
if $dvb_logs | grep "Refusing to do so, please check your configuratio"
then
  pass "Remote backups have not been deleted."
else
  fail "Remote backups would have been deleted: $dvb_logs"
fi

docker compose down --volumes
