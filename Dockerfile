FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/yarda-team/yadra-bridge/internal/version.Version=${VERSION}" \
    -o /proxy ./cmd/proxy

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /proxy /app/proxy
EXPOSE 8090
ENTRYPOINT ["/app/proxy"]
