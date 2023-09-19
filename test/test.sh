#!/bin/sh

set -e

MATCH_PATTERN=$1
IMAGE_TAG=${IMAGE_TAG:-canary}

sandbox="docker_volume_backup_test_sandbox"
tarball="$(mktemp -d)/image.tar.gz"

trap finish EXIT INT TERM

finish () {
  rm -rf $(dirname $tarball)
  if [ ! -z $(docker ps -aq --filter=name=$sandbox) ]; then
    docker rm -f $(docker stop $sandbox)
  fi
  if [ ! -z $(docker volume ls -q --filter=name="^${sandbox}\$") ]; then
    docker volume rm $sandbox
  fi
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
  docker_run_args="--name "$sandbox" --detach \
    --privileged \
    -v $(dirname $(pwd)):/code \
    -v $tarball:/cache/image.tar.gz \
    -v $sandbox:/var/lib/docker"

  if [ -z "$NO_IMAGE_CACHE" ]; then
    docker_run_args="$docker_run_args \
      -v "${sandbox}_image":/var/lib/docker/image \
      -v "${sandbox}_overlay2":/var/lib/docker/overlay2"
  fi

  docker run $docker_run_args offen/docker-volume-backup:test-sandbox

  retry_counter=0
  until docker exec $sandbox /bin/sh -c 'docker info' > /dev/null 2>&1; do
    if [ $retry_counter -gt 20 ]; then
      echo "Gave up waiting for Docker daemon to become ready after 20 attempts"
      exit 1
    fi

    if [ "$(docker inspect $sandbox --format '{{ .State.Running }}')" = "false" ]; then
      docker rm $sandbox
      docker run $docker_run_args offen/docker-volume-backup:test-sandbox
    fi

    sleep 0.5
    retry_counter=$((retry_counter+1))
  done

  docker exec $sandbox /bin/sh -c "docker load -i /cache/image.tar.gz"
  docker exec -e TEST_VERSION=$IMAGE_TAG $sandbox /bin/sh -c "/code/test/$test"

  docker rm $(docker stop $sandbox)
  docker volume rm $sandbox
  echo ""
  echo "$test passed"
  echo ""
done
