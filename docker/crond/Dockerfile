FROM alpine:3.23.4
ENV BUSYBOX_VERSION=1.37.0-r30 \
    CURL_VERSION=8.19.0-r0
RUN apk update && apk add --no-cache busybox-suid=${BUSYBOX_VERSION} curl=${CURL_VERSION}
ENTRYPOINT [ "/usr/sbin/crond" ]
CMD [ "-f", "-L", "/dev/stdout" ]
