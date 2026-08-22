# syntax=docker/dockerfile:1
FROM golang:alpine AS builder

WORKDIR /app

RUN apk add --no-cache git gcc musl-dev

ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/musubi-server ./cmd/musubi-server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/musubi-cli ./cmd/musubi-cli
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/mock-snmp-agent ./cmd/mock-snmp-agent

# Runtime container
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

COPY --from=builder /app/bin/musubi-server /usr/local/bin/musubi-server
COPY --from=builder /app/bin/musubi-cli /usr/local/bin/musubi-cli
COPY --from=builder /app/bin/mock-snmp-agent /usr/local/bin/mock-snmp-agent

EXPOSE 8080 162/udp

ENTRYPOINT ["/usr/local/bin/musubi-server"]
