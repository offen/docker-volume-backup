# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.17-alpine as builder

WORKDIR /app
COPY go.mod go.sum ./
COPY src/main.go ./src/main.go
RUN go build -o backup src/main.go

FROM alpine:3.14

WORKDIR /root

RUN apk add --update ca-certificates docker openrc gnupg
RUN update-ca-certificates
RUN rc-update add docker boot

COPY --from=builder /app/backup /usr/bin/backup

COPY src/entrypoint.sh /root/
RUN chmod +x entrypoint.sh

ENTRYPOINT ["/root/entrypoint.sh"]
