#!/bin/sh

set -e

cd $(dirname $0)

docker network create test_network
docker volume create backup_data
docker volume create app_data
# This volume is created to test whether empty directories are handled
# correctly. It is not supposed to hold any data.
docker volume create empty_data

docker run -d \
  --name minio \
  --network test_network \
  --env MINIO_ROOT_USER=test \
  --env MINIO_ROOT_PASSWORD=test \
  --env MINIO_ACCESS_KEY=test \
  --env MINIO_SECRET_KEY=GMusLtUmILge2by+z890kQ \
  -v backup_data:/data \
  minio/minio:RELEASE.2020-08-04T23-10-51Z server /data

docker exec minio mkdir -p /data/backup

docker run -d \
  --name offen \
  --network test_network \
  -v app_data:/var/opt/offen/ \
  offen/offen:latest

sleep 10

docker run --rm \
  --network test_network \
  -v app_data:/backup/app_data \
  -v empty_data:/backup/empty_data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env AWS_ACCESS_KEY_ID=test \
  --env AWS_SECRET_ACCESS_KEY=GMusLtUmILge2by+z890kQ \
  --env AWS_ENDPOINT=minio:9000 \
  --env AWS_ENDPOINT_PROTO=http \
  --env AWS_S3_BUCKET_NAME=backup \
  --env BACKUP_FILENAME=test.tar.gz \
  --env "BACKUP_FROM_SNAPSHOT=true" \
  --entrypoint backup \
  offen/docker-volume-backup:${TEST_VERSION:-canary}

docker run --rm -it \
  -v backup_data:/data alpine \
  ash -c 'tar -xvf /data/backup/test.tar.gz && test -f /backup/app_data/offen.db && test -d /backup/empty_data'

echo "[TEST:PASS] Found relevant files in untared remote backup."

# This test does not stop containers during backup. This is happening on
# purpose in order to cover this setup as well.
if [ "$(docker ps -q | wc -l)" != "2" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker ps
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

docker rm $(docker stop minio offen)
docker volume rm backup_data app_data
docker network rm test_network
