#!/usr/bin/env bash

set -euo pipefail

if (($# != 3)); then
  printf 'usage: %s AUR_REPOSITORY VERSION SOURCE_SHA256\n' "$0" >&2
  exit 2
fi

aur_repository="$(cd -- "$1" && pwd)"
version="$2"
checksum="$3"
pkgbuild="$aur_repository/PKGBUILD"

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  printf 'invalid stable version: %s\n' "$version" >&2
  exit 1
}
[[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || {
  printf 'invalid SHA-256 checksum: %s\n' "$checksum" >&2
  exit 1
}
[[ -f "$pkgbuild" ]] || {
  printf 'PKGBUILD not found: %s\n' "$pkgbuild" >&2
  exit 1
}

current_version="$(sed -n 's/^pkgver=//p' "$pkgbuild")"
current_pkgrel="$(sed -n 's/^pkgrel=//p' "$pkgbuild")"
[[ "$current_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  printf 'unsupported current AUR version: %s\n' "$current_version" >&2
  exit 1
}
[[ "$current_pkgrel" =~ ^[0-9]+$ ]] || {
  printf 'unsupported current AUR pkgrel: %s\n' "$current_pkgrel" >&2
  exit 1
}

if [[ "$current_version" != "$version" ]]; then
  newest_version="$(printf '%s\n%s\n' "$current_version" "$version" | sort -V | tail -n 1)"
  if [[ "$newest_version" == "$current_version" ]]; then
    printf 'refusing to downgrade AUR from %s to %s\n' "$current_version" "$version" >&2
    exit 1
  fi
  target_pkgrel=1
else
  target_pkgrel="$current_pkgrel"
fi

temporary="$(mktemp "$aur_repository/.PKGBUILD.XXXXXX")"
cleanup() {
  rm -f -- "$temporary"
}
trap cleanup EXIT

awk -v version="$version" -v pkgrel="$target_pkgrel" -v checksum="$checksum" '
  /^pkgver=/ {
    print "pkgver=" version
    next
  }
  /^pkgrel=/ {
    print "pkgrel=" pkgrel
    next
  }
  /^source=/ {
    print "source=(\"${pkgname}-${pkgver}.tar.gz::${url}/releases/download/v${pkgver}/${pkgname}-${pkgver}.tar.gz\")"
    next
  }
  /^sha256sums=/ {
    print "sha256sums=(\047" checksum "\047)"
    next
  }
  { print }
' "$pkgbuild" >"$temporary"

for field in pkgver pkgrel source sha256sums; do
  count="$(grep -c "^${field}=" "$temporary")"
  [[ "$count" -eq 1 ]] || {
    printf 'expected exactly one %s entry in PKGBUILD, found %s\n' "$field" "$count" >&2
    exit 1
  }
done

chmod --reference="$pkgbuild" "$temporary"
mv -f -- "$temporary" "$pkgbuild"
trap - EXIT
