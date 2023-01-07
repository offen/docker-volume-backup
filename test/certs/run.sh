#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

openssl genrsa -des3 -passout pass:test -out rootCA.key 4096
openssl req -passin pass:test \
  -subj "/C=DE/ST=BE/O=IntegrationTest, Inc." \
  -x509 -new -key rootCA.key -sha256 -days 1 -out rootCA.crt

openssl genrsa -out minio.key 4096
openssl req -new -sha256 -key minio.key \
  -subj "/C=DE/ST=BE/O=IntegrationTest, Inc./CN=minio" \
  -out minio.csr

openssl x509 -req -passin pass:test \
  -in minio.csr \
  -CA rootCA.crt -CAkey rootCA.key -CAcreateserial \
  -extfile san.cnf \
  -out minio.crt -days 1 -sha256

openssl x509 -in minio.crt -noout -text

docker compose up -d
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

docker run --rm -it \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'tar -xvf /minio_data/backup/test.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in untared remote backups."

docker compose down --volumes
