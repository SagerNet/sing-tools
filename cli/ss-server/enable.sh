#!/usr/bin/env bash

set -e -o pipefail

sudo systemctl enable ss
sudo systemctl start ss
sudo journalctl -u ss --output cat -f
