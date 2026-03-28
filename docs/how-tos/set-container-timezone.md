---
title: Set the timezone the container runs in
layout: default
parent: How Tos
nav_order: 8
---

# Set the timezone the container runs in

:warning: **Deprecation Warning**  
The method described below (bind-mounting files from the host) is **deprecated**. Please use the `TZ` environment variable (which uses the `tzdata` package) instead.

> ```yml
> services:
>   backup:
>     image: offen/docker-volume-backup:v2
>     volumes:
>       - data:/backup/my-app-backup:ro
>       - /etc/timezone:/etc/timezone:ro
>       - /etc/localtime:/etc/localtime:ro
>
> volumes:
>   data:
> ```

## Recommended approach (using `TZ`)

By default, the container runs in the UTC timezone. To configure a different timezone in a portable and reliable way, set the `TZ` environment variable to a valid [tz database time zone](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones):

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
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
