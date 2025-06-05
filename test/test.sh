#!/bin/sh

set -e

MATCH_PATTERN=$1
IMAGE_TAG=${IMAGE_TAG:-canary}

sandbox="docker_volume_backup_test_sandbox"
tarball="$(mktemp -d)/image.tar.gz"

trap finish EXIT INT TERM

finish () {
  rm -rf $(dirname $tarball)
  docker compose down
}

docker build -t offen/docker-volume-backup:test-sandbox .

if [ ! -z "$BUILD_IMAGE" ]; then
  docker build -t offen/docker-volume-backup:$IMAGE_TAG $(dirname $(pwd))
fi

docker save offen/docker-volume-backup:$IMAGE_TAG -o $tarball

find_args="-mindepth 1 -maxdepth 1 -type d"
if [ ! -z "$MATCH_PATTERN" ]; then
  find_args="$find_args -name $MATCH_PATTERN"
fi

for dir in $(find $find_args | sort); do
  dir=$(echo $dir | cut -c 3-)
  echo "################################################"
  echo "Now running ${dir}"
  echo "################################################"
  echo ""

  test="${dir}/run.sh"
  export TARBALL=$tarball
  export SOURCE=$(dirname $(pwd))
  docker compose up -d

  until $(docker compose exec manager /bin/sh -c 'docker info' > /dev/null 2>&1)
  do
    echo "Docker daemon not ready yet, retrying in 5s"
    sleep 5
  done

  docker compose exec manager /bin/sh -c "docker load -i /cache/image.tar.gz"
  docker compose exec -e TEST_VERSION=$IMAGE_TAG manager /bin/sh -c "/code/test/$test"

  docker compose down
  echo ""
  echo "$test passed"
  echo ""
done
