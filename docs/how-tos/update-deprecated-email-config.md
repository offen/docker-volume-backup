---
title: Update deprecated email configuration
layout: default
parent: How Tos
nav_order: 18
---

# Update deprecated email configuration

Starting with version 2.6.0, configuring email notifications using `EMAIL_*` keys has been deprecated.
Instead of providing multiple values using multiple keys, you can now provide a single URL for `NOTIFICATION_URLS`.

Before:
```ini
EMAIL_NOTIFICATION_RECIPIENT="you@example.com"
EMAIL_NOTIFICATION_SENDER="no-reply@example.com"
EMAIL_SMTP_HOST="posteo.de"
EMAIL_SMTP_PASSWORD="secret"
EMAIL_SMTP_USERNAME="me"
EMAIL_SMTP_PORT="587"
```

After:
```ini
NOTIFICATION_URLS=smtp://me:secret@posteo.de:587/?fromAddress=no-reply@example.com&toAddresses=you@example.com
```
