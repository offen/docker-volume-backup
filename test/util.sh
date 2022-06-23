#!/bin/sh

set -e

info () {
  echo "[TEST:INFO] "$1""
}

pass () {
  echo "[TEST:PASS] "$1""
}

fail () {
  echo "[TEST:FAIL] "$1""
  exit 1
}

expect_running_containers () {
  if [ "$(docker ps -q | wc -l)" != "$1" ]; then
    echo "[TEST:FAIL] Expected $1 containers to be running, instead seen:"
    docker-compose ps
    exit 1
  fi
  pass "$1 containers running."
}
