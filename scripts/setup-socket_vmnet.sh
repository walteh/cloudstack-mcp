#!/bin/bash

set -eux -o pipefail

tmpdir=$(mktemp -d)

function cleanup() {
	rm -rf "$tmpdir"
}
trap "cleanup" EXIT

VERSION="$(curl -fsSL https://api.github.com/repos/lima-vm/socket_vmnet/releases/latest | jq -r .tag_name)"
FILE="socket_vmnet-${VERSION:1}-$(uname -m).tar.gz"

# Download the binary archive
curl -SL "https://github.com/lima-vm/socket_vmnet/releases/download/${VERSION}/${FILE}" --output "$tmpdir/$FILE"

ls -laR "$tmpdir"

# (Optional) Attest the GitHub Artifact Attestation using GitHub's gh command (https://cli.github.com)
gh attestation verify --owner=lima-vm "$tmpdir/$FILE"

# (Optional) Preview the contents of the binary archive
tar tzvf "$tmpdir/$FILE"

# Install /opt/socket_vmnet from the binary archive
sudo tar Cxzvf / "$tmpdir/$FILE" opt/socket_vmnet

limactl sudoers >"$tmpdir/etc_sudoers.d_lima" && sudo install -o root "$tmpdir/etc_sudoers.d_lima" "/private/etc/sudoers.d/lima"

rm -rf "$tmpdir"
