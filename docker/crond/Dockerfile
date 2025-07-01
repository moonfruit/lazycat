FROM alpine:3.22.0
ENV BUSYBOX_VERSION=1.37.0-r18 \
    CURL_VERSION=8.14.1-r1
RUN apk update && apk add --no-cache busybox-suid=${BUSYBOX_VERSION} curl=${CURL_VERSION}
ENTRYPOINT [ "/usr/sbin/crond" ]
CMD [ "-f", "-L", "/dev/stdout" ]
COPY crontab /etc/crontabs/root
