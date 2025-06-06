# Integration Tests

## Running tests

The main entry point for running tests is the `./test.sh` script.
It can be used to run the entire test suite, or just a single test case.

### Run all tests

```sh
./test.sh
```

### Run a single test case

```sh
./test.sh <directory-name>
```

### Configuring a test run

In addition to the match pattern, which can be given as the first positional argument, certain behavior can be changed by setting environment variables:

#### `BUILD_IMAGE`

When set, the test script will build an up-to-date `docker-volume-backup` image from the current state of your source tree, and run the tests against it.

```sh
BUILD_IMAGE=1 ./test.sh
```

The default behavior is not to build an image, and instead look for a version on your host system.

#### `IMAGE_TAG`

Setting this value lets you run tests against different existing images, so you can compare behavior:

```sh
IMAGE_TAG=v2.30.0 ./test.sh
```

By default, two local images are created that persist the image data and provide it to containers at runtime.

## Understanding the test setup

The test setup runs each test case in an isolated Docker container, which itself is running an otherwise unused Docker daemon.
This means, tests can rely on noone else using that daemon, making expectations about the number of running containers and so forth.
As the sandbox container is also expected to be torn down post test, the scripts do not need to do any clean up or similar.

## Anatomy of a test case

The `test.sh` script looks for an exectuable file called `run.sh` in each directory.
When found, it is executed and signals success by returning a 0 exit code.
Any other exit code is considered a failure and will halt execution of further tests.

There is an `util.sh` file containing a few commonly used helpers which can be used by putting the following prelude to a new test case:

```sh
cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))
```

### Running tests in swarm mode

A test case can signal it wants to run in swarm mode by placing an empty `.swarm` file inside the directory.
In case the swarm setup should be compose of multiple nodes, a `.multinode` file can be used.

A multinode setup will contain one manager (`manager`) and two worker nodes (`worker1` and `worker2`).
