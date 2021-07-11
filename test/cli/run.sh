#!/bin/sh

set -e

cd $(dirname $0)

docker network create test_network
docker volume create backup_data
docker volume create app_data

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
  --label "docker-volume-backup.stop-during-backup=true" \
  -v app_data:/var/opt/offen/ \
  offen/offen:latest

sleep 10

docker run -d \
  --name backup \
  --network test_network \
  -v app_data:/backup/app_data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env AWS_ACCESS_KEY_ID=test \
  --env AWS_SECRET_ACCESS_KEY=GMusLtUmILge2by+z890kQ \
  --env AWS_ENDPOINT=minio:9000 \
  --env AWS_ENDPOINT_PROTO=http \
  --env AWS_S3_BUCKET_NAME=backup \
  --env BACKUP_FILENAME=test.tar.gz \
  --env BACKUP_CRON_EXPRESSION="0 0 5 31 2 ?" \
  offen/docker-volume-backup:$TEST_VERSION

docker exec backup backup

docker run --rm -it \
  -v backup_data:/data alpine \
  ash -c 'tar -xvf /data/backup/test.tar.gz && test -f /backup/app_data/offen.db'

echo "[TEST:PASS] Found relevant files in untared backup."

if [ "$(docker ps -q | wc -l)" != "3" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker ps
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

docker rm $(docker stop minio offen backup)
docker volume rm backup_data app_data
docker network rm test_network
