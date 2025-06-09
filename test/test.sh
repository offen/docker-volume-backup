#!/bin/sh

set -e

MATCH_PATTERN=$1
IMAGE_TAG=${IMAGE_TAG:-canary}

sandbox="docker_volume_backup_test_sandbox"
tarball="$(mktemp -d)/image.tar.gz"
compose_profile="default"

trap finish EXIT INT TERM

finish () {
  rm -rf $(dirname $tarball)
  docker compose --profile $compose_profile down
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

  if [ -f ${dir}/.multinode ]; then
    compose_profile="multinode"
  fi

  docker compose --profile $compose_profile up -d --wait
  test_context=manager
  if [ -f "${dir}/.multinode" ] && [ -s "${dir}/.multinode" ]; then
    test_context=$(cat $dir/.multinode)
    echo "Running tests on $test_context instead of manager"
  fi
  docker compose exec $test_context /bin/sh -c "docker load -i /cache/image.tar.gz"

  if [ -f "${dir}/.swarm" ]; then
    docker compose exec manager docker swarm init
  elif [ -f "${dir}/.multinode" ]; then
    docker compose exec manager docker swarm init
    manager_ip=$(docker compose exec manager docker node inspect $(docker compose exec manager docker node ls -q) --format '{{ .Status.Addr }}')
    token=$(docker compose exec manager docker swarm join-token -q worker)
    docker compose exec worker1 docker swarm join --token $token $manager_ip:2377
    docker compose exec worker2 docker swarm join --token $token $manager_ip:2377

    if [ "$test_context" != "manager" ]; then
      docker compose exec -w "/code/$dir" manager docker stack deploy --compose-file="docker-compose.yml" test_stack
    fi
  fi

  docker compose exec -e TEST_VERSION=$IMAGE_TAG $test_context /bin/sh -c "/code/$test"

  docker compose --profile $compose_profile down
  echo ""
  echo "$test passed"
  echo ""
done
