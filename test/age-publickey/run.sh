#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename "$(pwd)")

export LOCAL_DIR="$(mktemp -d)"

age-keygen >"$LOCAL_DIR/pk-a.txt"
PK_A="$(grep -E 'public key' <"$LOCAL_DIR/pk-a.txt" | cut -d: -f2 | xargs)"
age-keygen >"$LOCAL_DIR/pk-b.txt"
PK_B="$(grep -E 'public key' <"$LOCAL_DIR/pk-b.txt" | cut -d: -f2 | xargs)"

ssh-keygen -t ed25519 -m pem -f "$LOCAL_DIR/id_ed25519" -C "docker-volume-backup@local"
PK_C="$(cat $LOCAL_DIR/id_ed25519.pub)"

export BACKUP_AGE_PUBLIC_KEYS="$PK_A,$PK_B,$PK_C"

docker compose up -d --quiet-pull --wait

docker compose exec backup backup

expect_running_containers "2"

do_decrypt() {
  TMP_DIR=$(mktemp -d)
  age --decrypt -i "$1" -o "$LOCAL_DIR/decrypted.tar.gz" "$LOCAL_DIR/test.tar.gz.age"
  tar -xf "$LOCAL_DIR/decrypted.tar.gz" -C "$TMP_DIR"

  if [ ! -f "$TMP_DIR/backup/app_data/offen.db" ]; then
    fail "Could not find expected file in untared archive."
  fi
  rm -vf "$LOCAL_DIR/decrypted.tar.gz"

  pass "Found relevant files in decrypted and untared local backup."

  if [ ! -L "$LOCAL_DIR/test-latest.tar.gz.age" ]; then
    fail "Could not find local symlink to latest encrypted backup."
  fi
}

do_decrypt "$LOCAL_DIR/pk-a.txt"
do_decrypt "$LOCAL_DIR/pk-b.txt"
do_decrypt "$LOCAL_DIR/id_ed25519"
