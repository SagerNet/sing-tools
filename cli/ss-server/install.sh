#!/usr/bin/env bash

set -e -o pipefail

DIR=$(dirname "$0")
PROJECT=$DIR/../..
PATH="$PATH:$(go env GOPATH)/bin"

pushd $PROJECT
export GOAMD64=$(go run ./cli/goamd64)
go install -v -trimpath -ldflags "-s -w -buildid=" ./cli/ss-server
go install -v -trimpath -ldflags "-s -w -buildid=" ./cli/ss-server-gencfg
popd

sudo cp $(go env GOPATH)/bin/ss-server /usr/local/bin/
sudo mkdir -p /usr/local/etc/shadowsocks
sudo cp $DIR/config.json /usr/local/etc/shadowsocks/config.json
echo ">> /usr/local/etc/shadowsocks/config.json"
ss-server-gencfg > sudo tee /usr/local/etc/shadowsocks/config.json
sudo cat /usr/local/etc/shadowsocks/config.json
sudo cp $DIR/ss.service /etc/systemd/system
sudo systemctl daemon-reload
