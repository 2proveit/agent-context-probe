#!/bin/sh

set -eu

repository=${ACP_REPOSITORY:-2proveit/agent-context-probe}
install_dir=${ACP_INSTALL_DIR:-"$HOME/.local/bin"}
version=${ACP_VERSION:-latest}
archive_path=""
checksums_path=""

usage() {
    cat <<'EOF'
Usage: install.sh [--version VERSION] [--install-dir DIR]
                  [--archive FILE] [--checksums FILE]

Downloads and installs Agent Context Probe. --archive installs a local release
archive and is used by offline lifecycle tests. Existing binaries are replaced
atomically; configuration and data are not modified.
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --version)
            version=$2
            shift 2
            ;;
        --install-dir)
            install_dir=$2
            shift 2
            ;;
        --archive)
            archive_path=$2
            shift 2
            ;;
        --checksums)
            checksums_path=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown argument: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

temporary_dir=$(mktemp -d)
trap 'rm -rf -- "$temporary_dir"' EXIT HUP INT TERM

if [ -z "$archive_path" ]; then
    case "$(uname -s)" in
        Darwin) release_os=darwin ;;
        Linux) release_os=linux ;;
        *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) release_arch=x86_64 ;;
        arm64|aarch64) release_arch=arm64 ;;
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
    esac

    if [ "$version" = "latest" ]; then
        version=$(curl -fsSL "https://api.github.com/repos/${repository}/releases/latest" |
            sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
        if [ -z "$version" ]; then
            echo "Unable to determine the latest release." >&2
            exit 1
        fi
    fi
    tag=$version
    case "$tag" in v*) release_version=${tag#v} ;; *) tag="v${tag}"; release_version=$version ;; esac
    archive_name="agent-context-probe_${release_version}_${release_os}_${release_arch}.tar.gz"
    archive_path="$temporary_dir/$archive_name"
    checksums_path="$temporary_dir/agent-context-probe_checksums.txt"
    release_url="https://github.com/${repository}/releases/download/${tag}"
    curl -fsSL "$release_url/$archive_name" -o "$archive_path"
    curl -fsSL "$release_url/agent-context-probe_checksums.txt" -o "$checksums_path"
fi

if [ ! -f "$archive_path" ]; then
    echo "Release archive does not exist: $archive_path" >&2
    exit 1
fi

if [ -n "$checksums_path" ]; then
    archive_name=$(basename "$archive_path")
    expected=$(awk -v name="$archive_name" '$2 == name {print $1}' "$checksums_path")
    if [ -z "$expected" ]; then
        echo "Checksum entry is missing for $archive_name" >&2
        exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$archive_path" | awk '{print $1}')
    else
        actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
    fi
    if [ "$actual" != "$expected" ]; then
        echo "Checksum verification failed for $archive_name" >&2
        exit 1
    fi
fi

tar -xzf "$archive_path" -C "$temporary_dir"
binary_path=$(find "$temporary_dir" -type f -name agent-context-probe | head -n 1)
if [ -z "$binary_path" ]; then
    echo "The archive does not contain agent-context-probe." >&2
    exit 1
fi

mkdir -p "$install_dir"
staged="$install_dir/.agent-context-probe.new.$$"
cp "$binary_path" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$install_dir/agent-context-probe"

echo "Installed $install_dir/agent-context-probe"
echo "Configuration and data will use the operating system standard user directories."
