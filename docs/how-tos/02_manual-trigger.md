---
title: Trigger a backp manually
layout: default
parent: How Tos
nav_order: 8
---

# Trigger a backup manually

You can manually trigger a backup run outside of the defined cron schedule by executing the `backup` command inside the container:

```console
docker exec <container_ref> backup
```

If the container is configured to run multiple schedules, you can source the respective conf file before invoking the command:

```console
docker exec <container_ref> /bin/sh -c 'set -a; source /etc/dockervolumebackup/conf.d/myconf.env; set +a && backup'
```
