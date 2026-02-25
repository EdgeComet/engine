#!/bin/sh
: "${CONFIG_DIR:=/opt/edgecomet/configs}"
exec /opt/edgecomet/bin/render-service -c "${CONFIG_DIR}/render-service.yaml"