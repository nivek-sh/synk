#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Download and install synk, or install the binary contained next to this script.

Usage:
  bash install.sh [--dir PATH | --system] [--with-bw=snap | --with-bw=npm | --no-bw]

Options:
  --dir PATH      Install into PATH instead of ~/.local/bin.
  --system        Install into /usr/local/bin.
  --with-bw=snap  Use Snap for Bitwarden CLI (may request sudo).
  --with-bw=npm   Use npm for Bitwarden CLI (user-local).
  --no-bw         Skip Bitwarden CLI installation.
  -h, --help      Show this help.

On Linux, the installer automatically tries Snap or npm if bw is missing.
On Ubuntu, the standalone installer uses APT unless you set a custom directory.
The SYNK_INSTALL_DIR environment variable can also set the destination.
EOF
}

fail() {
  printf 'install.sh: %s\n' "$*" >&2
  exit 1
}

bw_available() {
  if command -v bw >/dev/null 2>&1; then
    return 0
  fi

  local bin_dir
  local bin_dirs=(/snap/bin)
  if [[ -n "${HOME:-}" ]]; then
    bin_dirs+=("$HOME/.local/bin")
  fi
  for bin_dir in "${bin_dirs[@]}"; do
    if [[ -f "$bin_dir/bw" && -x "$bin_dir/bw" ]]; then
      printf 'note: add %s to PATH so Synk can find bw\n' "$bin_dir"
      return 0
    fi
  done
  return 1
}

install_bw_with_snap() {
  if ! command -v snap >/dev/null 2>&1; then
    printf 'warning: installing bw with Snap requires snapd and the snap command\n' >&2
    return 1
  fi
  if (( EUID == 0 )); then
    snap install bw
  elif command -v sudo >/dev/null 2>&1; then
    sudo snap install bw
  else
    printf 'warning: installing bw with Snap requires sudo or root access\n' >&2
    return 1
  fi
}

install_bw_with_npm() {
  if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
    printf 'warning: installing bw with npm requires Node.js and npm on PATH\n' >&2
    return 1
  fi
  if (( EUID == 0 )) || [[ -z "${HOME:-}" ]]; then
    printf 'warning: install bw with npm as a regular user with HOME set\n' >&2
    return 1
  fi
  npm install --global --prefix "$HOME/.local" @bitwarden/cli
}

install_bitwarden_cli() {
  if [[ "$(uname -s)" != Linux ]]; then
    if [[ "$bw_method" != auto ]]; then
      printf 'warning: automatic Bitwarden CLI installation is available on Linux\n' >&2
    fi
    return 1
  fi

  local method
  local methods=("$bw_method")
  if [[ "$bw_method" == auto ]]; then
    case "$(uname -m)" in
      arm64|aarch64) methods=(npm snap) ;;
      *) methods=(snap npm) ;;
    esac
  fi

  for method in "${methods[@]}"; do
    case "$method" in
      snap)
        if ! install_bw_with_snap; then
          continue
        fi
        ;;
      npm)
        if ! install_bw_with_npm; then
          continue
        fi
        ;;
    esac
    if bw_available; then
      return 0
    fi
    printf 'warning: %s completed but bw was not found\n' "$method" >&2
  done
  return 1
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
binary="$script_dir/synk"
release_version="@SYNK_VERSION@"
download_base_url="${SYNK_DOWNLOAD_BASE_URL:-https://github.com/nivek-sh/synk/releases/download/v$release_version}"
install_dir="${SYNK_INSTALL_DIR:-}"
custom_install_dir=false
if [[ -n "$install_dir" ]]; then
  custom_install_dir=true
fi
if [[ -z "$install_dir" && -n "${HOME:-}" ]]; then
  install_dir="$HOME/.local/bin"
fi
download_dir=""
temporary=""
bw_method=auto
asset_type=archive

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
      custom_install_dir=true
      shift 2
      ;;
    --dir=*)
      install_dir="${1#*=}"
      custom_install_dir=true
      shift
      ;;
    --system)
      install_dir="/usr/local/bin"
      custom_install_dir=false
      shift
      ;;
    --with-bw=snap|--with-bw=npm)
      bw_method="${1#*=}"
      shift
      ;;
    --no-bw)
      bw_method=skip
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

if [[ ! -f "$binary" ]]; then
  [[ "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || \
    fail "this development copy has no embedded release version"

  command -v curl >/dev/null 2>&1 || fail "curl is required to download synk"

  case "$(uname -s)" in
    Linux) platform="linux" ;;
    Darwin) platform="macos" ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac

  if [[ "$platform" == linux && "$custom_install_dir" == false && -r /etc/os-release ]] &&
    grep -Fxq -e 'ID=ubuntu' -e 'ID="ubuntu"' -e "ID='ubuntu'" /etc/os-release; then
    command -v dpkg >/dev/null 2>&1 || fail "dpkg is required to detect the Ubuntu architecture"
    architecture="$(dpkg --print-architecture)"
    case "$architecture" in
      amd64|arm64) ;;
      *) fail "unsupported Ubuntu architecture: $architecture" ;;
    esac
    command -v apt-get >/dev/null 2>&1 || fail "apt-get is required to install the Ubuntu package"
    apt_command=(apt-get)
    if (( EUID != 0 )); then
      command -v sudo >/dev/null 2>&1 || fail "sudo is required to install the Ubuntu package (or use --dir PATH)"
      apt_command=(sudo apt-get)
    fi
    asset_type=deb
    asset="synk_${release_version}_${architecture}.deb"
  else
    command -v tar >/dev/null 2>&1 || fail "tar is required to extract synk"
    case "$(uname -m)" in
      x86_64|amd64) architecture="x86_64" ;;
      arm64|aarch64) architecture="arm64" ;;
      *) fail "unsupported architecture: $(uname -m)" ;;
    esac
    archive_root="synk-$release_version-$platform-$architecture"
    asset="$archive_root.tar.gz"
  fi

  download_dir="$(mktemp -d)"

  printf 'downloading %s\n' "$asset"
  curl --fail --silent --show-error --location --retry 3 --retry-all-errors \
    --output "$download_dir/$asset" \
    "$download_base_url/$asset"
  curl --fail --silent --show-error --location --retry 3 --retry-all-errors \
    --output "$download_dir/SHA256SUMS" \
    "$download_base_url/SHA256SUMS"

  expected_checksum="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$download_dir/SHA256SUMS")"
  [[ "$expected_checksum" =~ ^[0-9a-f]{64}$ ]] || \
    fail "no valid checksum found for $asset"

  if command -v sha256sum >/dev/null 2>&1; then
    actual_checksum="$(sha256sum "$download_dir/$asset" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    actual_checksum="$(shasum -a 256 "$download_dir/$asset" | awk '{ print $1 }')"
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
  [[ "$actual_checksum" == "$expected_checksum" ]] || fail "checksum verification failed for $asset"

  if [[ "$asset_type" == archive ]]; then
    tar -xzf "$download_dir/$asset" -C "$download_dir"
    binary="$download_dir/$archive_root/synk"
  fi
fi

if [[ "$asset_type" == deb ]]; then
  chmod 0755 "$download_dir"
  chmod 0644 "$download_dir/$asset"
  "${apt_command[@]}" install -y "$download_dir/$asset"
  target=/usr/bin/synk
  if active_synk="$(command -v synk)" && [[ ! "$active_synk" -ef "$target" ]]; then
    printf 'note: %s takes precedence over %s in PATH\n' "$active_synk" "$target"
  fi
else
  [[ -n "$install_dir" ]] || fail "no install directory; set HOME, SYNK_INSTALL_DIR, or --dir"
  [[ -f "$binary" ]] || fail "synk binary not found"
  [[ -x "$binary" ]] || fail "synk binary is not executable"

  "$binary" --version >/dev/null || fail "the bundled binary cannot run on this platform"

  mkdir -p -- "$install_dir" || fail "cannot create $install_dir"
  [[ -w "$install_dir" ]] || fail "cannot write to $install_dir (try: sudo bash install.sh --system)"

  target="$install_dir/synk"
  temporary="$(mktemp "$install_dir/.synk.XXXXXX")"

  install -m 0755 "$binary" "$temporary"
  mv -f -- "$temporary" "$target"
  temporary=""

  case ":${PATH:-}:" in
    *":$install_dir:"*) ;;
    *)
      printf 'note: add %s to PATH to run synk directly\n' "$install_dir"
      ;;
  esac
fi

printf 'installed synk to %s\n' "$target"
"$target" --version

if ! bw_available; then
  if [[ "$bw_method" == skip ]] || ! install_bitwarden_cli; then
    printf 'warning: Bitwarden CLI (bw) is required at runtime. See https://bitwarden.com/help/cli/\n' >&2
  fi
fi
command -v ssh >/dev/null 2>&1 || printf 'warning: OpenSSH client (ssh) is required at runtime\n' >&2

printf 'next step: synk init\n'
