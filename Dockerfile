FROM node:22-bookworm-slim AS dashboard_remote_builder

WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends libprotobuf-dev protobuf-compiler ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY . /src
RUN set -eux; \
    wa_src=/src; \
    if [ -d /src/wa-app ]; then wa_src=/src/wa-app; fi; \
    mkdir -p /app/wa-app; \
    cp -a "$wa_src/proto" /app/wa-app/proto; \
    cp -a "$wa_src/scripts" /app/wa-app/scripts; \
    chmod +x /app/wa-app/scripts/*.sh; \
    cp -a "$wa_src/webui" /app/wa-app/webui; \
    rm -rf /src
WORKDIR /app/wa-app/webui
RUN npm ci --prefer-offline --no-audit --fund=false && npm run build

FROM golang:1.26-alpine AS builder

WORKDIR /app/wa-app
RUN apk add --no-cache git ca-certificates protobuf-dev

COPY . /src
RUN set -eux; \
    wa_src=/src; \
    if [ -d /src/wa-app ]; then wa_src=/src/wa-app; fi; \
    cp -a "$wa_src/." /app/wa-app/; \
    rm -rf /src; \
    chmod +x scripts/*.sh; \
    go mod download
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2 \
    && scripts/generate-proto.sh \
    && CGO_ENABLED=0 GOOS=linux go build -o wa-app-service ./cmd/wa-app-service

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /app/wa-app/wa-app-service .
COPY --from=dashboard_remote_builder /app/wa-app/webui/dist /app/dashboard/wa
EXPOSE 50091 8080
CMD ["./wa-app-service"]
