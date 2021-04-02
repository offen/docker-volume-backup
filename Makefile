DOCKER_TAG ?= local

.PHONY: build
build:
	@docker build -t offen/docker-volume-backup:$(DOCKER_TAG) .
