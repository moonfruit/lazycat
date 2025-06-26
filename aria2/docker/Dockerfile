FROM alpine:3.22.0
ENV VERSION=1.37.0-r1
RUN apk update && apk add --no-cache aria2=${VERSION}
ENTRYPOINT [ "/usr/bin/aria2c" ]
