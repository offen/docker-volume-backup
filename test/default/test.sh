#!/bin/sh

set -e

cd $(dirname $0)

docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v default_backup_data:/data alpine \
  ash -c 'tar -xf /data/backup/test.tar.gz && test -f /backup/app_data/offen.db'

docker-compose down --volumes

echo "Test passed"
