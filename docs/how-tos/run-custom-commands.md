---
title: Run custom commands during the backup lifecycle
layout: default
nav_order: 5
parent: How Tos
---

# Run custom commands during the backup lifecycle

In certain scenarios it can be required to run specific commands before and after a backup is taken (e.g. dumping a database).
When mounting the Docker socket into the `docker-volume-backup` container, you can define pre- and post-commands that will be run in the context of the target container (it is also possible to run commands inside the `docker-volume-backup` container itself using this feature).

{: .important }
In a multi-node Swarm setup, commands can currently only be run on the node the `offen/docker-volume-backup` container is running on.
Labeled containers on other nodes are not visible to the backup command.

Such commands are defined by specifying the command in a `docker-volume-backup.[step]-[pre|post]` label where `step` can be any of the following phases of a backup lifecycle:

- `archive` (the tar archive is created)
- `process` (the tar archive is processed, e.g. encrypted - optional)
- `copy` (the tar archive is copied to all configured storages)
- `prune` (existing backups are pruned based on the defined ruleset - optional)

{: .note }
So that the `docker-volume-backup` container can access the labels on other containers, it is necessary that the docker socket is mounted into
the `docker-volume-backup` container as shown in the Quickstart example.

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

{: .note }
Due to Docker limitations, you currently cannot use any kind of redirection in these commands unless you pass the command to `/bin/sh -c` or similar.
I.e. instead of using `echo "ok" > ok.txt` you will need to use `/bin/sh -c 'echo "ok" > ok.txt'`.

If you have more than one `docker-volume-backup` container (possibly across several docker-compose environments) to backup or you are using
multiple backup schedules, you will need to use `EXEC_LABEL` in the configuration and a `docker-volume-backup.exec-label` label on each
container using custom commands to ensure that the commands are only run by the correct `docker-volume-backup` instance.

{: .important }
In case you use `EXEC_LABEL` together with configuration mounted from `conf.d` it's important to understand that a distinct `EXEC_LABEL` __should be set in each configuration__.
Else, schedules that do not specify an `EXEC_LABEL` will still trigger commands on all containers with such labels, no matter whether they specify `docker-volume-backup.exec-label` or not.

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
However, there are no guarantees about the order in which they are run, which could also happen concurrently.

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
