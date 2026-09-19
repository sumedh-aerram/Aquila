# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget && adduser -D -H -u 65532 shop
USER shop
COPY --from=build /out/service /usr/local/bin/service
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/service"]
