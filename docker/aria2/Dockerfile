FROM alpine:3.22.2
ENV ARIA2_VERSION=1.37.0-r1
RUN apk update && apk add --no-cache aria2=${ARIA2_VERSION}
ENTRYPOINT [ "/usr/bin/aria2c" ]
