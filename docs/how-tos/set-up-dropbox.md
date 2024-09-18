---
title: Set up Dropbox storage backend
layout: default
parent: How Tos
nav_order: 12
---

# Set up Dropbox storage backend

## Acquiring authentication tokens

1. Create a new Dropbox App in the [App Console](https://www.dropbox.com/developers/apps)
2. Open your new Dropbox App and set `DROPBOX_APP_KEY` and `DROPBOX_APP_SECRET` in your environment (e.g. docker-compose.yml) accordingly
3. Click on `Permissions` in your app and make sure, that the following permissions are cranted (or more):
  - `files.metadata.write`
  - `files.metadata.read`
  - `files.content.write`
  - `files.content.read`
4. Replace APPKEY in `https://www.dropbox.com/oauth2/authorize?client_id=APPKEY&token_access_type=offline&response_type=code` with the app key from step 2
5. Visit the URL and confirm the access of your app. This gives you an `auth code` -> save it somewhere!
6. Replace AUTHCODE, APPKEY, APPSECRET accordingly and perform the request:
```
curl https://api.dropbox.com/oauth2/token \
    -d code=AUTHCODE \
    -d grant_type=authorization_code \
    -d client_id=APPKEY \
    -d client_secret=APPSECRET
```
7. Execute the request. You will get a JSON formatted reply. Use the value of the `refresh_token` for the last environment variable `DROPBOX_REFRESH_TOKEN`
8. You should now have `DROPBOX_APP_KEY`, `DROPBOX_APP_SECRET` and `DROPBOX_REFRESH_TOKEN` set. These don't expire.

Note: Using the "Generated access token" in the app console is not supported, as it is only very short lived and therefore not suitable for an automatic backup solution. The refresh token handles this automatically - the setup procedure above is only needed once.

## Other parameters

Important: If you chose `App folder` access during the creation of your Dropbox app in step 1 above, `DROPBOX_REMOTE_PATH` will be a relative path under the App folder!
(_For example, DROPBOX_REMOTE_PATH=/somedir means the backup file will be uploaded to /Apps/myapp/somedir_)
On the other hand if you chose `Full Dropbox` access, the value for `DROPBOX_REMOTE_PATH` will represent an absolute path inside your Dropbox storage area.
(_Still considering the same example above, the backup file will be uploaded to /somedir in your Dropbox root_)
