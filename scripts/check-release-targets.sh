#!/bin/sh
# Builds every release target and checks that each one is static and correctly
# typed. A CGO dependency turns one of these into a dynamically linked binary
# that fails on a device with a different libc, so this is the check that keeps
# the release matrix honest.
set -eu

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

export CGO_ENABLED=0
export GOOS=linux

GOARCH=amd64 go build -o "$out/aegis-amd64" ./cmd/aegis
GOARCH=arm64 go build -o "$out/aegis-arm64" ./cmd/aegis
GOARCH=arm GOARM=7 go build -o "$out/aegis-armv7" ./cmd/aegis
GOARCH=arm GOARM=6 go build -o "$out/aegis-armv6" ./cmd/aegis

status=0

check_type() {
	file -b "$1" | grep -q "$2" || {
		echo "$1: expected $2, got: $(file -b "$1")" >&2
		status=1
	}
}

check_type "$out/aegis-arm64" "ARM aarch64"
check_type "$out/aegis-armv7" "ARM, EABI5"
check_type "$out/aegis-armv6" "ARM, EABI5"

for binary in "$out"/aegis-*; do
	if file -b "$binary" | grep -q "dynamically linked"; then
		echo "$binary is dynamically linked, which breaks the release matrix" >&2
		status=1
	fi
done

if [ "$status" -ne 0 ]; then
	exit 1
fi

echo "every release target builds static and correctly typed"
