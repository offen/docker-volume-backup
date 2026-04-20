---
title: Setting the Time Zone
layout: default
parent: How Tos
nav_order: 8
---

# Setting the Time Zone

## Use Environment Variable `TZ`

A container started using this image will default to UTC. To modify the time zone, set the `TZ` environment variable to a valid [tz database time zone](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones):

```yml
services:
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      - TZ=Europe/Berlin
    volumes:
      - data:/backup/my-app-backup:ro

volumes:
  data:
```

## Notes

This approach is preferred because it:

- avoids dependency on host configuration  
- works consistently across environments  

### Compatibility

- Bind-mounting timezone files will continue to work if `TZ` is not set.
- If `TZ` is set, it takes precedence over any bind-mounted timezone configuration.
- An invalid `TZ` value will cause the container to default to UTC.

:warning: **Deprecation Warning**  
The method described below (bind-mounting files from the host) is **deprecated**. Please use the new method described above (`TZ`)

> ```yml
> services:
>   backup:
>     image: offen/docker-volume-backup:latest
>     volumes:
>       - data:/backup/my-app-backup:ro
>       - /etc/timezone:/etc/timezone:ro
>       - /etc/localtime:/etc/localtime:ro
>       - /usr/share/zoneinfo:/usr/share/zoneinfo:ro
>
> volumes:
>   data:
> ```