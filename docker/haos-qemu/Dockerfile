FROM debian:trixie-slim

LABEL org.opencontainers.image.version=1.0.0

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates iproute2 libvdeplug-pcap ovmf \
        qemu-system-x86 qemu-utils xz-utils \
    && rm -rf /var/lib/apt/lists/*
