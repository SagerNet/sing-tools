#!/usr/bin/env bash

set -e -o pipefail

DIR=$(dirname "$0")
PROJECT=$DIR/../..
PATH="$PATH:$(go env GOPATH)/bin"

pushd $PROJECT
export GOAMD64=$(go run ./cli/goamd64)
go install -v -trimpath -ldflags "-s -w -buildid=" ./cli/genpsk
go install -v -trimpath -ldflags "-s -w -buildid=" ./cli/ss-relay
popd

sudo cp $(go env GOPATH)/bin/ss-server /usr/local/bin/
sudo mkdir -p /usr/local/etc/shadowsocks-relay
sudo cp $DIR/config.json /usr/local/etc/shadowsocks-relay/config.json
echo ">> /usr/local/etc/shadowsocks-relay/config.json"
sudo sed -i "s|psk|$(genpsk 16)|" /usr/local/etc/shadowsocks-relay/config.json
sudo cat /usr/local/etc/shadowsocks-relay/config.json
sudo cp $DIR/ss-relay.service /etc/systemd/system
sudo systemctl daemon-reload
