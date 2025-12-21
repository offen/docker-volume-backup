#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

GOTIFY_TOKEN=$(curl -sSLX POST -H 'Content-Type: application/json' -d '{"name":"test"}' http://admin:custom@localhost:8080/application | jq -r '.token')
info "Set up Gotify application using token $GOTIFY_TOKEN"

docker compose exec backup backup

NUM_MESSAGES=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages | length')
if [ "$NUM_MESSAGES" != 0 ]; then
  fail "Expected no notifications to be sent when not configured"
fi
pass "No notifications were sent when not configured."

docker compose down

NOTIFICATION_URLS="gotify://gotify/${GOTIFY_TOKEN}?disableTLS=true" docker compose up -d

docker compose exec backup backup

NUM_MESSAGES=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages | length')
if [ "$NUM_MESSAGES" != 1 ]; then
  fail "Expected one notifications to be sent when configured"
fi
pass "Correct number of notifications were sent when configured."

MESSAGE_TITLE=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages[0].title')
MESSAGE_BODY=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages[0].message')

if [ "$MESSAGE_TITLE" != "Successful test run with extra-value, yay!" ]; then
  fail "Unexpected notification title $MESSAGE_TITLE"
fi
pass "Custom notification title was used."

if [ "$MESSAGE_BODY" != "Backing up /tmp/test.tar.gz succeeded." ]; then
  fail "Unexpected notification body $MESSAGE_BODY"
fi
pass "Custom notification body was used."

NUM_MESSAGES_BEFORE=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages | length')
docker compose exec -e AWS_S3_BUCKET_NAME=missing-bucket -e AWS_ACCESS_KEY_ID_FILE=/tmp/missing backup backup \
  && fail "Expected backup to fail due to missing AWS_ACCESS_KEY_ID_FILE."
pass "Backup failed with missing AWS_ACCESS_KEY_ID_FILE as expected."

NUM_MESSAGES_AFTER=$(curl -sSL http://admin:custom@localhost:8080/message | jq -r '.messages | length')
if [ "$NUM_MESSAGES_AFTER" != "$((NUM_MESSAGES_BEFORE + 1))" ]; then
  fail "Expected one additional notification after failure, got $NUM_MESSAGES_AFTER total."
fi
pass "Failure notification was sent."
