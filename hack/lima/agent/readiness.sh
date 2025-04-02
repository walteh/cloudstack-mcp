#!/bin/bash

set -eux -o pipefail

# libvirt
if ! timeout 30s bash -c "until systemctl list-units --type=service --state=running | grep -q 'libvirtd.service'; do sleep 3; done"; then
	echo >&2 "libvirt is not running yet"
	exit 1
fi

# cloudstack-agent
if ! timeout 30s bash -c "until systemctl list-units --type=service --state=running | grep -q 'cloudstack-agent.service'; do sleep 3; done"; then
	echo >&2 "cloudstack-agent is not running yet"
	exit 1
fi

exit 0
