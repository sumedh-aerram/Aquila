# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY examples/shop/go.mod examples/shop/go.sum ./examples/shop/
RUN go -C examples/shop mod download

COPY . .
ARG VERSION=dev
RUN mkdir -p /out \
	&& go run ./cmd/sourceindex -dir examples/shop -out /out/source.json \
	&& go build -trimpath -ldflags="-s -w -X github.com/sumedhaerram/aquila/internal/version.Version=${VERSION}" -o /out/aquila-server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache wget \
	&& adduser -D -H -u 65532 aquila
COPY --from=build --chown=65532:65532 /out/aquila-server /usr/local/bin/aquila-server
COPY --from=build --chown=65532:65532 /out/source.json /etc/aquila/source.json
USER aquila
EXPOSE 8080 8091
ENTRYPOINT ["/usr/local/bin/aquila-server"]
