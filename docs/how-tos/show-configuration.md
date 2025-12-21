---
title: Show loaded configuration
layout: default
parent: How Tos
nav_order: 8
---

# Show loaded configuration

You can print the configuration that `docker-volume-backup` has picked up without running a backup:

```console
docker exec <container_ref> backup show-config
```

If configuration sourcing fails, the error is printed to stdout to aid debugging.

If you want to test a one-off value, pass it directly:

```console
docker exec -e BACKUP_SOURCES=/backup -e NOTIFICATION_URLS=stdout:// <container_ref> backup show-config
```

Note: output includes secrets exactly as loaded.
