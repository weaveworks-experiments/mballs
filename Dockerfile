FROM golang:1.10-alpine AS build

RUN mkdir /out
RUN mkdir -p /out/etc/apk && cp -r /etc/apk/* /out/etc/apk/
RUN apk add --no-cache --initdb --root /out \
    alpine-baselayout \
    busybox \
    ca-certificates \
    curl \
    && true

ENV APP $GOPATH/src/github.com/weaveworks-experiments/multicast-demo
RUN mkdir -p "$(dirname ${APP})"
COPY . $APP

WORKDIR $APP
RUN go build \
    && cp multicast-demo /out/usr/local/bin/multicast-demo

WORKDIR /out

FROM scratch
CMD skaffold
COPY --from=build  /out /
CMD []
ENTRYPOINT ["multicast-demo"]
