FROM debian:trixie-slim@sha256:8aa5a7bf5a1955bddbe7d74fcdd0b91ab30e74362a8b73af672b0481b35b44a0

ARG QEMU_VERSION=1:10.0.13+ds-0+deb13u1
LABEL org.opencontainers.image.version="${QEMU_VERSION}"

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates iproute2 libvdeplug-pcap ovmf \
        "qemu-system-x86=${QEMU_VERSION}" "qemu-utils=${QEMU_VERSION}" xz-utils \
    && rm -rf /var/lib/apt/lists/*
