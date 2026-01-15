---
title: Show loaded configuration
layout: default
parent: How Tos
nav_order: 8
---

# Print loaded configuration

You can print the configuration that `docker-volume-backup` has picked up without running a backup:

```console
docker exec <container_ref> backup print-config
```

If configuration sourcing fails, the error is printed to stdout to aid debugging.

If you want to test a one-off value, pass it directly:

```console
docker exec -e BACKUP_SOURCES=/backup -e NOTIFICATION_URLS=stdout:// <container_ref> backup print-config
```
{: .note }
Output includes secrets exactly as loaded.

{: .warning }
This feature is still in development and might change in future releases.
