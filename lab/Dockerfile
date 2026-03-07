# Multi-stage build for netbird-events (eventsproc)
# Build context: repository root (../ from lab/)

# Stage 1: Build
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=$(cat version.txt)" -o /eventsproc ./cmd/main.go

# Stage 2: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /eventsproc /usr/local/bin/eventsproc

RUN mkdir -p /etc/app/eventsproc
EXPOSE 2113

ENTRYPOINT ["/usr/local/bin/eventsproc", "--config", "/etc/app/eventsproc/config.yaml"]
