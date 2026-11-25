# syntax=docker/dockerfile:1
FROM debian:trixie-slim

ARG QEMU_VERSION=10.0.13
LABEL org.opencontainers.image.version="${QEMU_VERSION}"

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# hadolint ignore=DL3008
RUN --mount=type=bind,source=package-version.sh,target=/tmp/package-version.sh \
    apt-get -o APT::Update::Error-Mode=any update \
    && PACKAGE_VERSION=$(bash /tmp/package-version.sh "${QEMU_VERSION}") \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates iproute2 libvdeplug-pcap ovmf xz-utils \
        "qemu-system-x86=${PACKAGE_VERSION}" "qemu-utils=${PACKAGE_VERSION}" \
    && ACTUAL_VERSION=$(qemu-system-x86_64 --version | awk 'NR == 1 {print $4}') \
    && test "${ACTUAL_VERSION}" = "${QEMU_VERSION}" \
    && rm -rf /var/lib/apt/lists/* \
    && rm -f /var/log/apt/* /var/log/dpkg.log /var/log/alternatives.log /var/cache/ldconfig/aux-cache

# cspell:words dpkg ldconfig libvdeplug ovmf
