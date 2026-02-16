# Stage 1: Build the SAM binary
FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /sam ./cmd/web/

# Stage 2: Download pocketd binary
FROM alpine:3.21 AS pocketd-downloader

ARG POCKETD_VERSION=v0.1.31
ARG TARGETARCH

RUN apk add --no-cache curl && \
    ARCH=${TARGETARCH} && \
    if [ "$ARCH" = "amd64" ]; then ARCH="amd64"; fi && \
    if [ "$ARCH" = "arm64" ]; then ARCH="arm64"; fi && \
    curl -fSL "https://github.com/pokt-network/poktroll/releases/download/${POCKETD_VERSION}/poktroll_linux_${ARCH}.tar.gz" \
      -o /tmp/poktroll.tar.gz && \
    tar -xzf /tmp/poktroll.tar.gz -C /tmp && \
    mv /tmp/poktrolld /usr/local/bin/pocketd && \
    chmod +x /usr/local/bin/pocketd

# Stage 3: Runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 1000 sam

WORKDIR /app

COPY --from=builder /sam ./sam
COPY --from=pocketd-downloader /usr/local/bin/pocketd /usr/local/bin/pocketd
COPY web/ ./web/

RUN mkdir -p /app/data && chown -R sam:sam /app

ENV PORT=9999 \
    CONFIG_FILE=/app/config.yaml \
    DATA_DIR=/app/data

EXPOSE 9999

VOLUME ["/app/data"]

USER sam

ENTRYPOINT ["./sam"]
