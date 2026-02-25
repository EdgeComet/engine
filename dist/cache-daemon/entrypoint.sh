#!/bin/sh
: "${CONFIG_DIR:=/opt/edgecomet/configs}"
exec /opt/edgecomet/bin/cache-daemon -c "${CONFIG_DIR}/cache-daemon.yaml"