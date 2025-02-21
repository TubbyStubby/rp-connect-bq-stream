FROM golang:1.23 AS build

ENV CGO_ENABLED=0
ENV GOOS=linux
RUN useradd -u 10001 connect

WORKDIR /go/src/github.com/TubbyStubby/rp-connect-bq-stream/
# Update dependencies: On unchanged dependencies, cached layer will be reused
COPY go.* /go/src/github.com/TubbyStubby/rp-connect-bq-stream/
RUN go mod download

# Build
COPY . /go/src/github.com/TubbyStubby/rp-connect-bq-stream/

# Tag timetzdata required for busybox base image:
# https://github.com/redpanda-data/connect/issues/897
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod go build -tags timetzdata -ldflags="-w -s" -o connect

# Pack
FROM busybox AS package

LABEL maintainer="TubbyStubby <tubbystubby2@gmail.com>"
LABEL org.opencontainers.image.source="https://github.com/TubbyStubby/rp-connect-bq-stream"

WORKDIR /

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /go/src/github.com/TubbyStubby/rp-connect-bq-stream/connect .
COPY ./config/example_1.yaml /connect.yaml

USER connect

EXPOSE 4195

ENTRYPOINT ["/connect"]

CMD ["-c", "/connect.yaml"]
