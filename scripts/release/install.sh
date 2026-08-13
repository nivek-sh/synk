#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Download and install synk, or install the binary contained next to this script.

Usage:
  ./install.sh [--dir PATH]
  sudo ./install.sh --system

Options:
  --dir PATH   Install into PATH instead of ~/.local/bin.
  --system     Install into /usr/local/bin.
  -h, --help   Show this help.

The SYNK_INSTALL_DIR environment variable can also set the destination.
EOF
}

fail() {
  printf 'install.sh: %s\n' "$*" >&2
  exit 1
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
binary="$script_dir/synk"
release_version="@SYNK_VERSION@"
download_base_url="${SYNK_DOWNLOAD_BASE_URL:-https://github.com/nivek-sh/synk/releases/download/v$release_version}"
install_dir="${SYNK_INSTALL_DIR:-}"
if [[ -z "$install_dir" && -n "${HOME:-}" ]]; then
  install_dir="$HOME/.local/bin"
fi
download_dir=""
temporary=""

cleanup() {
  if [[ -n "$temporary" ]]; then
    rm -f -- "$temporary"
  fi
  if [[ -n "$download_dir" ]]; then
    rm -rf -- "$download_dir"
  fi
}
trap cleanup EXIT

while (($# > 0)); do
  case "$1" in
    --dir)
      (($# >= 2)) || fail "--dir requires a path"
      install_dir="$2"
      shift 2
      ;;
    --dir=*)
      install_dir="${1#*=}"
      shift
      ;;
    --system)
      install_dir="/usr/local/bin"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$install_dir" ]] || fail "no install directory; set HOME, SYNK_INSTALL_DIR, or --dir"

if [[ ! -f "$binary" ]]; then
  [[ "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || \
    fail "this development copy has no embedded release version"

  command -v curl >/dev/null 2>&1 || fail "curl is required to download synk"
  command -v tar >/dev/null 2>&1 || fail "tar is required to extract synk"

  case "$(uname -s)" in
    Linux) platform="linux" ;;
    Darwin) platform="macos" ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) architecture="x86_64" ;;
    arm64|aarch64) architecture="arm64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac

  archive_root="synk-$release_version-$platform-$architecture"
  archive="$archive_root.tar.gz"
  download_dir="$(mktemp -d)"

  printf 'downloading synk %s for %s-%s\n' "$release_version" "$platform" "$architecture"
  curl --fail --silent --show-error --location --retry 3 --retry-all-errors \
    --output "$download_dir/$archive" \
    "$download_base_url/$archive"
  curl --fail --silent --show-error --location --retry 3 --retry-all-errors \
    --output "$download_dir/SHA256SUMS" \
    "$download_base_url/SHA256SUMS"

  expected_checksum="$(awk -v archive="$archive" '$2 == archive { print $1 }' "$download_dir/SHA256SUMS")"
  [[ "$expected_checksum" =~ ^[0-9a-f]{64}$ ]] || \
    fail "no valid checksum found for $archive"

  if command -v sha256sum >/dev/null 2>&1; then
    actual_checksum="$(sha256sum "$download_dir/$archive" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    actual_checksum="$(shasum -a 256 "$download_dir/$archive" | awk '{ print $1 }')"
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
  [[ "$actual_checksum" == "$expected_checksum" ]] || fail "checksum verification failed for $archive"

  tar -xzf "$download_dir/$archive" -C "$download_dir"
  binary="$download_dir/$archive_root/synk"
fi

[[ -f "$binary" ]] || fail "synk binary not found"
[[ -x "$binary" ]] || fail "synk binary is not executable"

"$binary" --version >/dev/null || fail "the bundled binary cannot run on this platform"

mkdir -p -- "$install_dir" || fail "cannot create $install_dir"
[[ -w "$install_dir" ]] || fail "cannot write to $install_dir (try: sudo ./install.sh --system)"

target="$install_dir/synk"
temporary="$(mktemp "$install_dir/.synk.XXXXXX")"

install -m 0755 "$binary" "$temporary"
mv -f -- "$temporary" "$target"
temporary=""

printf 'installed synk to %s\n' "$target"
"$target" --version

case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *)
    printf 'note: add %s to PATH to run synk directly\n' "$install_dir"
    ;;
esac

command -v bw >/dev/null 2>&1 || printf 'warning: Bitwarden CLI (bw) is required at runtime\n' >&2
command -v ssh >/dev/null 2>&1 || printf 'warning: OpenSSH client (ssh) is required at runtime\n' >&2

printf 'next step: synk init\n'
