#!/bin/sh

set -e

TEST_VERSION=${1:-canary}

for dir in $(ls -d -- */); do
  test="${dir}run.sh"
  echo "################################################"
  echo "Now running $test"
  echo "################################################"
  echo ""
  TEST_VERSION=$TEST_VERSION /bin/sh $test
  echo ""
  echo "$test passed"
  echo ""
done
