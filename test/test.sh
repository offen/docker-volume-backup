#!/bin/sh

set -e

for dir in $(ls -d -- */); do
  test="${dir}run.sh"
  echo "################################################"
  echo "Now running $test"
  echo "################################################"
  echo ""
  /bin/sh $test
  echo ""
  echo "$test passed"
  echo ""
done
