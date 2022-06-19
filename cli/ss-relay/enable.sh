#!/usr/bin/env bash

set -e -o pipefail

sudo systemctl enable ss-relay
sudo systemctl start ss-relay
sudo journalctl -u ss-relay --output cat -f
