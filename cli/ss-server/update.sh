#!/usr/bin/env bash

set -e -o pipefail

DIR=$(dirname "$0")
PROJECT=$DIR/../..

pushd $PROJECT
export GOAMD64=$(go run ./cli/goamd64)
git fetch
git reset FETCH_HEAD --hard
git clean -fdx
go install -v -trimpath -ldflags "-s -w -buildid=" ./cli/ss-server
popd

sudo systemctl stop ss
sudo cp $(go env GOPATH)/bin/ss-server /usr/local/bin
sudo systemctl start ss
sudo journalctl -u ss --output cat -f
