#!/bin/bash

# This script repacks the docker-compose-v2 tarball in order to include all of
# the vendor modules needed to build the application offline.
#
# It works with uscan v4 and assumes that it will be called like:
#
#   SCRIPT --upstream-version VERSION
#
# Author: Sergio Durigan Junior <sergio.durigan@canonical.com>

set -e
set -x
set -o pipefail

GOBIN=${GOBIN:-go}

upstream_version="$2"
orig_tar=$(realpath "../docker-compose-v2_${upstream_version}.orig.tar.xz")
orig_dir="$PWD"
work_dir=$(mktemp -d)

cleanup ()
{
    cd "$orig_dir"
    "$GOBIN" clean -cache -modcache
    rm -rf "$work_dir"
}

trap cleanup INT QUIT 0

export GOPATH="$work_dir/.gopath"
export GOCACHE="$work_dir/.gocache"

printf "Unpacking tarball '$orig_tar' to '$work_dir'"

tar xf "$orig_tar" -C "$work_dir"
source_dir_name=$(ls -1 "$work_dir")
cd "$work_dir/$source_dir_name"
"$GOBIN" mod vendor
cd ..
tar cJf "$orig_tar" "$source_dir_name"

cleanup

exit 0
