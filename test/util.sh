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

skip () {
  echo "[test:${current_test:-none}:skip] "$1""
  exit 0
}

expect_running_containers () {
  if [ "$(docker ps -q | wc -l)" != "$1" ]; then
    fail "Expected $1 containers to be running, instead seen: "$(docker ps -q | wc -l)""
  fi
  pass "$1 containers running."
}

docker() {
  case $1 in
    compose)
      shift
      case $1 in
        up)
          shift
          command docker compose up --timeout 3 "$@";;
        down)
          shift
          command docker compose down --timeout 3 "$@";;
        *)
          command docker compose "$@";;
      esac
      ;;
    *)
      command docker "$@";;
  esac
}
