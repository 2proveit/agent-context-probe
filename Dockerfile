# Build the SPA assets. Node.js is not present in the runtime image.
FROM --platform=$BUILDPLATFORM node:20-alpine AS node-builder

WORKDIR /app

# Copy package files
COPY web/package*.json ./web/
WORKDIR /app/web
RUN npm ci

# Copy web source code and build
COPY web/ ./
RUN npm run build

# Embed the SPA assets in the Go executable.
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS go-builder

WORKDIR /app
RUN apk add --no-cache git
COPY proxy/go.mod proxy/go.sum ./proxy/
WORKDIR /app/proxy
RUN go mod download
COPY proxy/ ./
COPY --from=node-builder /app/proxy/internal/webui/dist/client ./internal/webui/dist/client
ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH
RUN mkdir -p /app/bin && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -ldflags "-s -w -X github.com/seifghazi/claude-code-monitor/internal/buildinfo.Version=${VERSION} -X github.com/seifghazi/claude-code-monitor/internal/buildinfo.Commit=${GIT_COMMIT} -X github.com/seifghazi/claude-code-monitor/internal/buildinfo.BuildTime=${BUILD_TIME}" \
      -o /app/bin/agent-context-probe ./cmd/agent-context-probe

# Single-process production runtime.
FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ca-certificates wget

# Create app user for security
RUN addgroup -g 1001 -S appgroup && \
    adduser -S appuser -u 1001 -G appgroup

COPY --from=go-builder /app/bin/agent-context-probe /usr/local/bin/agent-context-probe

# Create data directory for SQLite database
RUN mkdir -p /app/data && chown -R appuser:appgroup /app

# Environment variables with defaults
ENV ACP_HOST=0.0.0.0
ENV PORT=3001
ENV READ_TIMEOUT=600s
ENV WRITE_TIMEOUT=600s
ENV IDLE_TIMEOUT=600s
ENV ANTHROPIC_FORWARD_URL=https://api.anthropic.com
ENV ANTHROPIC_VERSION=2023-06-01
ENV ANTHROPIC_MAX_RETRIES=3
ENV DB_PATH=/app/data/requests.db

EXPOSE 3001

# Switch to app user
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:3001/health > /dev/null || exit 1

CMD ["agent-context-probe", "start"]
