FROM alpine:3.24.1
ENV BUSYBOX_VERSION=1.37.0-r31 \
    CURL_VERSION=8.20.0-r1
RUN apk update && apk add --no-cache busybox-suid=${BUSYBOX_VERSION} curl=${CURL_VERSION}
ENTRYPOINT [ "/usr/sbin/crond" ]
CMD [ "-f", "-L", "/dev/stdout" ]
