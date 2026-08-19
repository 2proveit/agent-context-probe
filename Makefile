.PHONY: all build build-proxy build-web run clean install dev run-proxy run-web db-reset release-check release-snapshot help

VERSION ?= dev
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO_PACKAGE = github.com/seifghazi/claude-code-monitor/internal/buildinfo
LDFLAGS = -X $(BUILDINFO_PACKAGE).Version=$(VERSION) -X $(BUILDINFO_PACKAGE).Commit=$(GIT_COMMIT) -X $(BUILDINFO_PACKAGE).BuildTime=$(BUILD_TIME)

# Default target
all: install build

# Install dependencies
install:
	@echo "📦 Installing Go dependencies..."
	cd proxy && go mod download
	@echo "📦 Installing Node dependencies..."
	cd web && npm ci

# Build the static dashboard first, then embed it in the Go executable.
build: build-proxy

build-proxy: build-web
	@echo "🔨 Building Agent Context Probe..."
	mkdir -p bin
	cd proxy && go build -ldflags "$(LDFLAGS)" -o ../bin/agent-context-probe ./cmd/agent-context-probe

build-web:
	@echo "🔨 Building web interface..."
	cd web && npm run build

# Run in development mode
dev:
	@echo "🚀 Starting development servers..."
	./run.sh

# Run proxy only
run-proxy: build-web
	cd proxy && go run ./cmd/agent-context-probe start

# Run web only
run-web:
	cd web && npm run dev

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf bin/
	rm -rf web/build/
	rm -rf web/dist/
	rm -rf web/.cache/
	rm -rf proxy/internal/webui/dist/client/
	rm -rf proxy/internal/webui/dist/server/
	rm -rf dist/

# Database operations
db-reset:
	@test -n "$(DATA_DIR)" || (echo "Set DATA_DIR to the exact data directory to reset." >&2; exit 1)
	@echo "🗑️  Resetting $(DATA_DIR)/requests.db..."
	rm -f "$(DATA_DIR)/requests.db"

release-check:
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean --skip=publish

# Help
help:
	@echo "Agent Context Probe - Available targets:"
	@echo "  make install    - Install all dependencies"
	@echo "  make build      - Build the static dashboard and single executable"
	@echo "  make dev        - Build and run the single executable"
	@echo "  make run-proxy  - Run proxy server only"
	@echo "  make run-web    - Run web interface only"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make db-reset   - Reset database"
	@echo "  make release-check    - Validate .goreleaser.yaml"
	@echo "  make release-snapshot - Build local release artifacts"
	@echo "  make help       - Show this help message"
