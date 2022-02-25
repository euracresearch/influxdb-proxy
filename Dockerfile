FROM golang:1.17.7 as builder
ARG browser_ref
ARG browser_sha
ENV BUILD_DIR /tmp/proxy

ADD . ${BUILD_DIR}
WORKDIR ${BUILD_DIR}

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X 'main.version=${browser_ref}' -X 'main.commit=${browser_sha}'" -o proxy proxy.go

FROM alpine:latest
RUN apk add --no-cache iputils ca-certificates net-snmp-tools procps &&\
    update-ca-certificates
COPY --from=builder /tmp/proxy/proxy /usr/bin/proxy
EXPOSE 8080
CMD ["proxy"]