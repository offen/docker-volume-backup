ARG version=canary
FROM offen/docker-volume-backup:$version

RUN addgroup -g 10001 -S nonroot \
  && adduser -u 10000 -S -G nonroot -h /home/nonroot nonroot
