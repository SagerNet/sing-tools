#!/usr/bin/env bash

sudo systemctl stop ss-relay
sudo systemctl stop 'ss-relay@*'
sudo rm -rf /usr/local/bin/ss-relay
sudo rm -rf /usr/local/etc/shadowsocks-relay
sudo rm -rf /etc/systemd/system/ss-relay.service
sudo systemctl daemon-reload
