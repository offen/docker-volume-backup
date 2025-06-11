#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

docker stack deploy --compose-file=docker-compose.yml test_stack
