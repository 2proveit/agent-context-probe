#!/bin/sh

set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)

npm --prefix "$repo_root/web" ci
npm --prefix "$repo_root/web" run typecheck
npm --prefix "$repo_root/web" test -- --run
npm --prefix "$repo_root/web" run build
(
    cd "$repo_root/proxy"
    go test ./...
)
