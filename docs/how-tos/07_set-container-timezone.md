---
title: Set the timezone the container runs in
layout: default
parent: How Tos
nav_order: 7
---

# Set the timezone the container runs in

By default a container based on this image will run in the UTC timezone.
As the image is designed to be as small as possible, additional timezone data is not included.
In case you want to run your cron rules in your local timezone (respecting DST and similar), you can mount your Docker host's `/etc/timezone` and `/etc/localtime` in read-only mode:

```yml
version: '3'

services:
  backup:
    image: offen/docker-volume-backup:v2
    volumes:
      - data:/backup/my-app-backup:ro
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro

volumes:
  data:
```
