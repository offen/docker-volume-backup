---
title: Replace deprecated BACKUP_STOP_CONTAINER_LABEL setting
layout: default
parent: How Tos
nav_order: 19
---

# Replace deprecated `BACKUP_STOP_CONTAINER_LABEL` setting

Version `v2.36.0` deprecated the `BACKUP_STOP_CONTAINER_LABEL` setting and renamed it `BACKUP_STOP_DURING_BACKUP_LABEL` which is supposed to signal that this will stop both containers _and_ services.
Migrating is done by renaming the key for your custom value:

```diff
    env:
-     BACKUP_STOP_CONTAINER_LABEL: database
+     BACKUP_STOP_DURING_BACKUP_LABEL: database
```

The old key will stay supported until the next major version, but logs a warning each time a backup is taken.
