#!/bin/sh
# Installs or upgrades the budget service. Run as root from a directory holding
# the linux binary (budget) and budget.service:  sudo sh install.sh
set -e
cd "$(dirname "$0")"

if ! id budget >/dev/null 2>&1; then
	useradd --system --home-dir /var/lib/budget --no-create-home --shell /usr/sbin/nologin budget
fi
install -m 0755 -o root -g root budget /usr/local/bin/budget
install -m 0644 -o root -g root budget.service /etc/systemd/system/budget.service
systemctl daemon-reload
systemctl enable budget >/dev/null
systemctl restart budget
sleep 1
systemctl --no-pager --lines=5 status budget
