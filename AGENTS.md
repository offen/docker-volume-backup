# AGENTS instructions

## Project overview
- Go command-line tool and Docker image for backing up Docker volumes to local or remote storage.

## Setup
- Requires Go 1.25+ and Docker.
- Install Go dependencies: `go mod download`
- Optional: use `nix-shell` to enter the provided development environment.
- Docs site depends on Ruby and Bundler.

## Build
- Build the CLI: `go build ./cmd/backup`
- Build the Docker image: `docker build -t offen/docker-volume-backup:dev .`

## Code style
- Format Go code with `gofmt` or `go fmt`.
- Lint with `golangci-lint run` (configured in `.golangci.yml`).
- EditorConfig enforces LF line endings, final newline and tabs in Go files.

## Testing
- Unit tests: `go test ./...`
- Integration tests: `cd test && ./test.sh`
  - Use `BUILD_IMAGE=1` to build the image from source before running tests.
  - Run a single test with `./test.sh <directory-name>`

## Docs
- Build docs locally: `cd docs && bundle install && bundle exec jekyll serve`

## Pull requests
- Run `golangci-lint run`, `go test ./...` and the integration tests before submitting.
- Keep commits focused and include meaningful commit messages.
