#!/bin/sh

set -e

info () {
  echo "[test:${current_test:-none}:info] "$1""
}

pass () {
  echo "[test:${current_test:-none}:pass] "$1""
}

fail () {
  echo "[test:${current_test:-none}:fail] "$1""
  exit 1
}

expect_running_containers () {
  if [ "$(docker ps -q | wc -l)" != "$1" ]; then
    fail "Expected $1 containers to be running, instead seen: "$(docker ps -a | wc -l)""
  fi
  pass "$1 containers running."
}
