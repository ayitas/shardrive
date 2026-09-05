#!/bin/sh
set -eu

mkdir -p /data/storage
chown shardrive:shardrive /data/storage
chmod 0750 /data/storage

exec su-exec shardrive "$@"
