#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh

ssh-keygen -t rsa -m pem -b 4096 -N "test1234" -f id_rsa -C "docker-volume-backup@local"

docker-compose up -d
sleep 5

docker-compose exec backup backup

sleep 5

expect_running_containers 3

docker run --rm -it \
  -v ssh_backup_data:/ssh_data \
  alpine \
  ash -c 'tar -xvf /ssh_data/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in decrypted and untared remote backups."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v ssh_backup_data:/ssh_data \
  alpine \
  ash -c '[ $(find /ssh_data/ -type f | wc -l) = "1" ]'

pass "Remote backups have not been deleted."

docker-compose down --volumes
rm -f id_rsa id_rsa.pub
