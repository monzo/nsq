#!/bin/bash

# The dist job in .github/workflows/ci.yml calls this script on every push to
# master to package the linux tarballs and publish them, so the steps below are
# only needed for a full local build across every platform.
#
# 1. commit to bump the version and update the changelog/readme
# 2. tag that commit
# 3. use dist.sh to produce tar.gz for linux and darwin
# 4. upload *.tar.gz to our bitly s3 bucket
# 5. docker push nsqio/nsq
# 6. push to nsqio/master
# 7. update the release metadata on github / upload the binaries there too
# 8. update the gh-pages branch with versions / download links
# 9. update homebrew version
# 10. send release announcement emails
# 11. update IRC channel topic
# 12. tweet

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Overridable so CI can package just the platforms it publishes, and skip the
# work it has already done. The defaults reproduce a full local release build.
DIST_OS="${DIST_OS:-linux darwin freebsd windows}"
DIST_ARCH="${DIST_ARCH:-$(go env GOARCH)}"
DIST_TESTS="${DIST_TESTS:-1}"
DIST_DOCKER="${DIST_DOCKER:-1}"

rm -rf   $DIR/dist/docker
mkdir -p $DIR/dist/docker

BLDFLAGS='-ldflags="-s -w"'
version=$(awk '/const Binary/ {print $NF}' < $DIR/internal/version/binary.go | sed 's/"//g')
goversion=$(go version | awk '{print $3}')

# Record the archived files as root-owned without needing to chown them first,
# which keeps this script runnable without sudo. GNU tar and the bsdtar that
# ships with macOS spell these flags differently.
if tar --version 2>/dev/null | grep -q '^tar (GNU tar)'; then
    TAROWNER=(--numeric-owner --owner=0 --group=0)
else
    TAROWNER=(--numeric-owner --uid 0 --gid 0 --uname root --gname root)
fi

if [ "$DIST_TESTS" == "1" ]; then
    echo "... running tests"
    ./test.sh
fi

for os in $DIST_OS; do
for arch in $DIST_ARCH; do
    echo "... building v$version for $os/$arch"
    BUILD=$(mktemp -d ${TMPDIR:-/tmp}/nsq-XXXXX)
    TARGET="nsq-$version.$os-$arch.$goversion"
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 \
        make DESTDIR=$BUILD PREFIX=/$TARGET BLDFLAGS="$BLDFLAGS" install
    pushd $BUILD
    if [ "$os" == "linux" ] && [ "$arch" == "amd64" ]; then
        cp -r $TARGET/bin $DIR/dist/docker/
    fi
    tar "${TAROWNER[@]}" -czf $TARGET.tar.gz $TARGET
    mv $TARGET.tar.gz $DIR/dist
    popd
    make clean
    rm -r $BUILD
done
done

if [ "$DIST_DOCKER" == "1" ]; then
    docker build -t nsqio/nsq:v$version .
    if [[ ! $version == *"-"* ]]; then
        echo "Tagging nsqio/nsq:v$version as the latest release."
        docker tag nsqio/nsq:v$version nsqio/nsq:latest
    fi
fi
