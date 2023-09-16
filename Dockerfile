# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.21-alpine as builder

WORKDIR /app
COPY . .
RUN go mod download
WORKDIR /app/cmd/backup
RUN go build -o backup .

FROM alpine:3.18

WORKDIR /root

RUN addgroup -g 10001 -S nonroot \
  && adduser -u 10000 -S -G nonroot -h /home/nonroot nonroot \
  && chmod a+rw /var/lock

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/cmd/backup/backup /usr/bin/backup
COPY --chmod=755 ./entrypoint.sh /usr/bin

ENTRYPOINT ["/usr/bin/entrypoint.sh"]
