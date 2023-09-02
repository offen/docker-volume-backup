#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export CERT_DIR=$(mktemp -d)

openssl genrsa -des3 -passout pass:test -out "$CERT_DIR/rootCA.key" 4096
openssl req -passin pass:test \
  -subj "/C=DE/ST=BE/O=IntegrationTest, Inc." \
  -x509 -new -key "$CERT_DIR/rootCA.key" -sha256 -days 1 -out "$CERT_DIR/rootCA.crt"

openssl genrsa -out "$CERT_DIR/minio.key" 4096
openssl req -new -sha256 -key "$CERT_DIR/minio.key" \
  -subj "/C=DE/ST=BE/O=IntegrationTest, Inc./CN=minio" \
  -out "$CERT_DIR/minio.csr"

openssl x509 -req -passin pass:test \
  -in "$CERT_DIR/minio.csr" \
  -CA "$CERT_DIR/rootCA.crt" -CAkey "$CERT_DIR/rootCA.key" -CAcreateserial \
  -extfile san.cnf \
  -out "$CERT_DIR/minio.crt" -days 1 -sha256

openssl x509 -in "$CERT_DIR/minio.crt" -noout -text

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'tar -xvf /minio_data/backup/test.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in untared remote backups."
