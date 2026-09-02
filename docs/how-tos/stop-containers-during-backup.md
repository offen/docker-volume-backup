---
title: Stop containers during backup
layout: default
parent: How Tos
nav_order: 1
---

# Stop containers during backup

{: .note }
In case you are running Docker in Swarm mode, [dedicated documentation](./use-with-docker-swarm.html) on service and container restart applies.

In many cases, it will be desirable to stop the services that are consuming the volume you want to backup in order to ensure data integrity.
This image can automatically stop and restart containers and services.
By default, any container that is labeled `docker-volume-backup.stop-during-backup=true` will be stopped before the backup is being taken and restarted once it has finished.

In case you need more fine grained control about which containers should be stopped (e.g. when backing up multiple volumes on different schedules), you can set the `BACKUP_STOP_DURING_BACKUP_LABEL` environment variable and then use the same value for labeling:

```yml
services:
  app:
    # definition for app ...
    labels:
      - docker-volume-backup.stop-during-backup=service1

  backup:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_STOP_DURING_BACKUP_LABEL: service1
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

## Target a single container from multiple backup instances

A Docker label can only hold a single value per key, so a container can carry
only one `docker-volume-backup.stop-during-backup` value. If you run multiple
instances of this image with different `BACKUP_STOP_DURING_BACKUP_LABEL` values
and want the same container to be stopped by more than one of them, set
`BACKUP_LABEL_MATCH_BEHAVIOR` to `one-of`. The container's label value is then split on
commas and matches if any of the entries equals the configured value. If your values
themselves contain a comma, set `BACKUP_LABEL_MATCH_SEPARATOR` to a different character
to split on instead.

```yml
services:
  app:
    # definition for app ...
    labels:
      - docker-volume-backup.stop-during-backup=service1,service2

  backup-one:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_STOP_DURING_BACKUP_LABEL: service1
      BACKUP_LABEL_MATCH_BEHAVIOR: one-of
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

  backup-two:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_STOP_DURING_BACKUP_LABEL: service2
      BACKUP_LABEL_MATCH_BEHAVIOR: one-of
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

The default value `match` keeps the previous behavior and requires the label
value to be equal to the configured value.

## Stop containers during backup without restarting

Sometimes you might want to stop containers for the backup but not have them start again automatically, for example if they are normally started by an external process or scheduler.

For this use case, you can use the label `docker-volume-backup.stop-during-backup-no-restart`.  
This label is **mutually exclusive** with `docker-volume-backup.stop-during-backup` and performs the same stop operation but skips restarting the container after the backup has finished.

```yml
services:
  app:
    # definition for app ...
    labels:
      - docker-volume-backup.stop-during-backup-no-restart=service2

  backup:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_STOP_DURING_BACKUP__NO_RESTART_LABEL: service2
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```
