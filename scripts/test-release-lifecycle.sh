#!/bin/sh

set -eu

PATH=/usr/bin:/bin
export PATH

if [ "$#" -ne 1 ]; then
    echo "Usage: test-release-lifecycle.sh RELEASE_ARCHIVE" >&2
    exit 2
fi

archive=$1
case "$archive" in /*) ;; *) archive=$(cd "$(dirname "$archive")" && pwd)/$(basename "$archive") ;; esac
script_dir=$(cd "$(dirname "$0")" && pwd)
test_root=$(mktemp -d)
server_pid=""
cleanup() {
    if [ -n "$server_pid" ]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
    rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

export HOME="$test_root/home"
export XDG_CONFIG_HOME="$test_root/config"
export XDG_DATA_HOME="$test_root/data"
install_dir="$test_root/bin"
readonly_cwd="$test_root/readonly"
mkdir -p "$HOME" "$readonly_cwd"
chmod 0555 "$readonly_cwd"

for tool in go node npm; do
    if command -v "$tool" >/dev/null 2>&1; then
        echo "Clean lifecycle environment unexpectedly contains $tool" >&2
        exit 1
    fi
done

sh "$script_dir/install.sh" --archive "$archive" --install-dir "$install_dir"
binary="$install_dir/agent-context-probe"
version_output=$("$binary" version)
echo "$version_output" | grep -q 'agent-context-probe'
if echo "$version_output" | grep -q 'agent-context-probe dev'; then
    echo "Release binary contains the development version." >&2
    exit 1
fi

start_and_check() {
    port=$1
    (
        cd "$readonly_cwd"
        "$binary" start --port "$port" >"$test_root/server.log" 2>&1
    ) &
    server_pid=$!
    attempts=0
    until {
        if command -v wget >/dev/null 2>&1; then
            wget -qO- "http://127.0.0.1:$port/health" >/dev/null 2>&1
        else
            curl -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1
        fi
    }; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge 30 ]; then
            cat "$test_root/server.log" >&2
            exit 1
        fi
        sleep 1
    done
    kill "$server_pid"
    wait "$server_pid" 2>/dev/null || true
    server_pid=""
}

start_and_check 39101
case "$(uname -s)" in
    Darwin) database="$HOME/Library/Application Support/Agent Context Probe/requests.db" ;;
    *) database="$XDG_DATA_HOME/agent-context-probe/requests.db" ;;
esac
test -f "$database"
test ! -e "$readonly_cwd/requests.db"

# Simulate an interrupted/invalid previous installation, then verify that the
# installer atomically replaces it while preserving the data directory.
printf 'broken executable\n' >"$binary"
chmod 0755 "$binary"
sh "$script_dir/install.sh" --archive "$archive" --install-dir "$install_dir"
"$binary" version | grep -q 'agent-context-probe'
start_and_check 39102
test -f "$database"

sh "$script_dir/uninstall.sh" --install-dir "$install_dir"
test ! -e "$binary"
test -f "$database"
echo "Release lifecycle test passed without Go, Node.js, or npm."
