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
