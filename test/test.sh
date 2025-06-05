#!/bin/sh

set -e

MATCH_PATTERN=$1
IMAGE_TAG=${IMAGE_TAG:-canary}

sandbox="docker_volume_backup_test_sandbox"
tarball="$(mktemp -d)/image.tar.gz"
compose_file="docker-compose.yml"

trap finish EXIT INT TERM

finish () {
  rm -rf $(dirname $tarball)
  docker compose -f $compose_file down
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

  if [ -f ${dir}/.swarm ]; then
    compose_file="swarm.yml"
  fi

  docker compose -f $compose_file up -d

  until $(docker compose exec manager /bin/sh -c 'docker info' > /dev/null 2>&1)
  do
    echo "Docker daemon not ready yet, retrying in 2s"
    sleep 2
  done

  if [ -f ${dir}/.swarm ]; then
    docker compose exec manager docker swarm init
    manager_ip=$(docker compose exec manager docker node inspect $(docker compose exec manager docker node ls -q) --format '{{ .Status.Addr }}')
    token=$(docker compose exec manager docker swarm join-token -q worker)
    docker compose exec worker1 docker swarm join --token $token manager:2377
    docker compose exec worker2 docker swarm join --token $token manager:2377
  fi

  docker compose exec manager /bin/sh -c "docker load -i /cache/image.tar.gz"
  docker compose exec -e TEST_VERSION=$IMAGE_TAG manager /bin/sh -c "/code/test/$test"

  docker compose -f $compose_file down
  echo ""
  echo "$test passed"
  echo ""
done
