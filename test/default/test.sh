#!/bin/sh

set -e

cd $(dirname $0)

docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v default_backup_data:/data alpine \
  ash -c 'tar -xf /data/backup/test.tar.gz && test -f /backup/app_data/offen.db'

if [ "$(docker-compose ps -q | wc -l)" != "3" ]; then
  echo "Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi

docker-compose down --volumes

echo "Test passed"
