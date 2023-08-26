#!/bin/sh

set -e

sandbox="docker_volume_backup_test_sandbox"

trap finish EXIT INT TERM

finish () {
  rm -rf $(dirname $tarball)
  if [ ! -z $(docker ps -aq --filter=name=$sandbox) ]; then
    docker rm -f $(docker stop $sandbox)
  fi
}

MATCH_PATTERN=$1
IMAGE_TAG=${IMAGE_TAG:-canary}

docker build -t offen/docker-volume-backup:test-sandbox .

if [ ! -z "$BUILD_IMAGE" ]; then
  docker build -t offen/docker-volume-backup:$IMAGE_TAG $(dirname $(pwd))
fi

tarball="$(mktemp -d)/image.tar.gz"
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
  docker run --name "$name" --detach \
    --privileged \
    --name $sandbox \
    -v $(dirname $(pwd)):/code \
    -v $tarball:/cache/image.tar.gz \
    offen/docker-volume-backup:test-sandbox

  until docker exec $sandbox /bin/sh -c 'docker info' > /dev/null 2>&1; do
    sleep 0.5
  done

  docker exec $sandbox /bin/sh -c "docker load -i /cache/image.tar.gz"
  docker exec -e TEST_VERSION=$IMAGE_TAG $sandbox /bin/sh -c "/bin/sh /code/test/$test"

  docker rm $(docker stop $sandbox)
  echo ""
  echo "$test passed"
  echo ""
done
