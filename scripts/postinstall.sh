#!/bin/sh

chown netbird:netbird -R /opt/app/eventsproc
chmod 755 /opt/app/eventsproc
chown -R netbird:netbird /etc/app/eventsproc
chmod 750 /etc/app/eventsproc

# Reload systemd to recognize the new unit file
systemctl daemon-reload

# uncomment to start automatically after install
#systemctl enable eventsproc
#systemctl start eventsproc
