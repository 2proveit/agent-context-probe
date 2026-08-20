#!/bin/sh

set -eu

if ! command -v go >/dev/null 2>&1; then
    echo "Go 1.21 or newer is required to build from source." >&2
    exit 1
fi
if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
    echo "Node.js 20 or newer and npm are required to build from source." >&2
    exit 1
fi

node_major=$(node -p 'process.versions.node.split(".")[0]')
if [ "$node_major" -lt 20 ]; then
    echo "Node.js 20 or newer is required. Found: $(node --version)" >&2
    exit 1
fi

if [ ! -d web/node_modules ]; then
    npm --prefix web ci
fi

make build
if [ -f config.yaml ]; then
    exec ./bin/agent-context-probe start --config config.yaml "$@"
fi
exec ./bin/agent-context-probe start "$@"
