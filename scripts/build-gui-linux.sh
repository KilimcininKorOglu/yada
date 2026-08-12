#!/bin/sh
# Builds the interface binary for Linux inside a golang container.
#
# glfw compiles against the target's X11, Wayland and OpenGL headers, and no
# cross toolchain ships them, so a container is the only way to produce a Linux
# build from a macOS machine. CI does not use this: it builds on a native Linux
# runner, where the same packages are installed by the workflow.
#
# Expects VERSION and TARGETARCH in the environment, /src mounted read-only and
# /out writable.
set -eu

export DEBIAN_FRONTEND=noninteractive

apt-get update -qq
apt-get install -y -qq --no-install-recommends \
    gcc libc6-dev pkg-config \
    libgl1-mesa-dev xorg-dev \
    libwayland-dev wayland-protocols libxkbcommon-dev libdecor-0-dev >/dev/null

cd /src

go build -trimpath -ldflags "-X main.version=${VERSION}" \
    -o "/out/unbound-dns-gui-linux-${TARGETARCH}" ./cmd/unbound-dns
