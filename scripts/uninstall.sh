#!/bin/sh

set -eu

install_dir=${ACP_INSTALL_DIR:-"$HOME/.local/bin"}
if [ "${1:-}" = "--install-dir" ]; then
    install_dir=$2
    shift 2
fi
if [ "$#" -ne 0 ]; then
    echo "Usage: uninstall.sh [--install-dir DIR]" >&2
    exit 2
fi

binary="$install_dir/agent-context-probe"
if [ -e "$binary" ]; then
    rm -f "$binary"
    echo "Removed $binary"
else
    echo "Agent Context Probe is not installed at $binary"
fi
echo "Configuration, request history, and database backups were preserved."
