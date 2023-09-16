---
title: Run multiple backup schedules in the same container
layout: default
parent: How Tos
nav_order: 11
---

# Run multiple backup schedules in the same container

Multiple backup schedules with different configuration can be configured by mounting an arbitrary number of configuration files (using the `.env` format) into `/etc/dockervolumebackup/conf.d`:

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./configuration:/etc/dockervolumebackup/conf.d

volumes:
  data:
```

A separate cronjob will be created for each config file.
If a configuration value is set both in the global environment as well as in the config file, the config file will take precedence.
The `backup` command expects to run on an exclusive lock, so in case you provide the same or overlapping schedules in your cron expressions, the runs will still be executed serially, one after the other.
The exact order of schedules that use the same cron expression is not specified.
In case you need your schedules to overlap, you need to create a dedicated container for each schedule instead.
When changing the configuration, you currently need to manually restart the container for the changes to take effect.

Set `BACKUP_SOURCES` for each config file to control which subset of volume mounts gets backed up:

```yml
# With a volume configuration like this:
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
  - ./configuration:/etc/dockervolumebackup/conf.d
  - app1_data:/backup/app1_data:ro
  - app2_data:/backup/app2_data:ro
```

```ini
# In the 1st config file:
BACKUP_SOURCES=/backup/app1_data

# In the 2nd config file:
BACKUP_SOURCES=/backup/app2_data
```
