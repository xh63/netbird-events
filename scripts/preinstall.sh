#!/bin/sh

getent group netbird >/dev/null || groupadd -r netbird
getent passwd netbird >/dev/null || useradd -m -r -g netbird -d /opt/netbird-prov -s /sbin/nologin netbird
