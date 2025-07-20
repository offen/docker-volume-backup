#!/bin/sh

set -e

echo "DEBUG: Script started"
cd "$(dirname "$0")"
echo "DEBUG: Changed directory"
. ../util.sh
echo "DEBUG: Sourced util.sh"
current_test=$(basename $(pwd))
echo "DEBUG: Set current_test=$current_test"

export SPEC_FILE=$(mktemp -d)/googledrive_v3.yaml
echo "DEBUG: Created SPEC_FILE=$SPEC_FILE"
cp googledrive_v3.yaml $SPEC_FILE
echo "DEBUG: Copied googledrive_v3.yaml"
sed -i 's/SERVER_MODIFIED_1/'"$(date "+%Y-%m-%dT%H:%M:%SZ")/g" $SPEC_FILE
echo "DEBUG: Replaced SERVER_MODIFIED_1"
sed -i 's/SERVER_MODIFIED_2/'"$(date "+%Y-%m-%dT%H:%M:%SZ" -d "14 days ago")/g" $SPEC_FILE
echo "DEBUG: Replaced SERVER_MODIFIED_2"

docker compose up -d --quiet-pull
echo "DEBUG: docker compose up done"
sleep 5
set +e
echo "DEBUG: Running backup"
logs=$(docker compose exec backup backup)
set -e
echo "DEBUG: Ran backup"

sleep 5

echo "DEBUG: Checking running containers"
expect_running_containers "4"
echo "DEBUG: Checked running containers"

echo "$logs"
if echo "$logs" | grep -q "ERROR"; then
  fail "Backup failed, errors reported: $logs"
else
  pass "Backup succeeded, no errors reported."
fi

echo "DEBUG: Starting retention=0 test"
# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d
echo "DEBUG: docker compose up for retention=0 done"
sleep 5

logs=$(docker compose exec -T backup backup)
echo "DEBUG: Ran backup for retention=0"

echo "$logs"
if echo "$logs" | grep -q "Refusing to do so, please check your configuration"; then
  pass "Remote backups have not been deleted."
else
  fail "Remote backups would have been deleted: $logs"
fi

echo "DEBUG: Starting retention=7 test"
# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)
BACKUP_RETENTION_DAYS="7" docker compose up -d
echo "DEBUG: docker compose up for retention=7 done"
sleep 5

info "Create second backup and prune"
logs=$(docker compose exec -T backup backup)
echo "DEBUG: Ran backup for retention=7"

echo "$logs"
if echo "$logs" | grep -q "Pruned 1 out of 2 backups as they were older"; then
  pass "Old remote backup has been pruned, new one is still present."
elif echo "$logs" | grep -q "ERROR"; then
  fail "Pruning failed, errors reported: $logs"
elif echo "$logs" | grep -q "None of 1 existing backups were pruned"; then
  fail "Pruning failed, old backup has not been pruned: $logs"
else
  fail "Pruning failed, unknown result: $logs"
fi
