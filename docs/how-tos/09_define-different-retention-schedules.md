---
title: Define different retention schedules
layout: default
parent: How Tos
nav_order: 9
---

# Define different retention schedules

If you want to manage backup retention on different schedules, the most straight forward approach is to define a dedicated configuration for retention rule using a different prefix in the `BACKUP_FILENAME` parameter and then run them on different cron schedules.

For example, if you wanted to keep daily backups for 7 days, weekly backups for a month, and retain monthly backups forever, you could create three configuration files and mount them into `/etc/dockervolumebackup/conf.d`:

```ini
# 01daily.conf
BACKUP_FILENAME="daily-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every day at 2am
BACKUP_CRON_EXPRESSION="0 2 * * *"
BACKUP_PRUNING_PREFIX="daily-backup-"
BACKUP_RETENTION_DAYS="7"
```

```ini
# 02weekly.conf
BACKUP_FILENAME="weekly-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every monday at 3am
BACKUP_CRON_EXPRESSION="0 3 * * 1"
BACKUP_PRUNING_PREFIX="weekly-backup-"
BACKUP_RETENTION_DAYS="31"
```

```ini
# 03monthly.conf
BACKUP_FILENAME="monthly-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every 1st of a month at 4am
BACKUP_CRON_EXPRESSION="0 4 1 * *"
```

Note that while it's possible to define colliding cron schedules for each of these configurations, you might need to adjust the value for `LOCK_TIMEOUT` in case your backups are large and might take longer than an hour.
