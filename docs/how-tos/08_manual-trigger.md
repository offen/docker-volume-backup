---
title: Trigger a backp manually
layout: default
parent: How Tos
nav_order: 8
---

# Trigger a backup manually

You can manually trigger a backup run outside of the defined cron schedule by executing the `backup` command inside the container:

```
docker exec <container_ref> backup
```
