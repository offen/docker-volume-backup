---
title: Run custom commands during the backup lifecycle
layout: default
nav_order: 4
parent: How Tos
---

# Run custom commands during the backup lifecycle

In certain scenarios it can be required to run specific commands before and after a backup is taken (e.g. dumping a database).
When mounting the Docker socket into the `docker-volume-backup` container, you can define pre- and post-commands that will be run in the context of the target container (it is also possible to run commands inside the `docker-volume-backup` container itself using this feature).
Such commands are defined by specifying the command in a `docker-volume-backup.[step]-[pre|post]` label where `step` can be any of the following phases of a backup lifecyle:

- `archive` (the tar archive is created)
- `process` (the tar archive is processed, e.g. encrypted - optional)
- `copy` (the tar archive is copied to all configured storages)
- `prune` (existing backups are pruned based on the defined ruleset - optional)

Taking a database dump using `mysqldump` would look like this:

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  database:
    image: mariadb
    volumes:
      - backup_data:/tmp/backups
    labels:
      - docker-volume-backup.archive-pre=/bin/sh -c 'mysqldump --all-databases > /backups/dump.sql'

volumes:
  backup_data:
```

Due to Docker limitations, you currently cannot use any kind of redirection in these commands unless you pass the command to `/bin/sh -c` or similar.
I.e. instead of using `echo "ok" > ok.txt` you will need to use `/bin/sh -c 'echo "ok" > ok.txt'`.

If you need fine grained control about which container's commands are run, you can use the `EXEC_LABEL` configuration on your `docker-volume-backup` container:

```yml
version: '3'

services:
  database:
    image: mariadb
    volumes:
      - backup_data:/tmp/backups
    labels:
      - docker-volume-backup.archive-pre=/bin/sh -c 'mysqldump --all-databases > /tmp/volume/dump.sql'
      - docker-volume-backup.exec-label=database

  backup:
    image: offen/docker-volume-backup:v2
    environment:
      EXEC_LABEL: database
    volumes:
      - data:/backup/dump:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  backup_data:
```


The backup procedure is guaranteed to wait for all `pre` or `post` commands to finish before proceeding.
However there are no guarantees about the order in which they are run, which could also happen concurrently.

By default the backup command is executed by the user provided by the container's image.
It is possible to specify a custom user that is used to run commands in dedicated labels with the format `docker-volume-backup.[step]-[pre|post].user`:

```yml
version: '3'

services:
  gitea:
    image: gitea/gitea
    volumes:
      - backup_data:/tmp
    labels:
      - docker-volume-backup.archive-pre.user=git
      - docker-volume-backup.archive-pre=/bin/bash -c 'cd /tmp; /usr/local/bin/gitea dump -c /data/gitea/conf/app.ini -R -f dump.zip'
```

Make sure the user exists and is present in `passwd` inside the target container.
