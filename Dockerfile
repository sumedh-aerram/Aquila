# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/sumedhaerram/aquila/internal/version.Version=${VERSION}" -o /out/aquila-server ./cmd/server

FROM alpine:3.21
RUN adduser -D -H -u 65532 aquila
USER aquila
COPY --from=build /out/aquila-server /usr/local/bin/aquila-server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/aquila-server"]
