#!/usr/bin/env bash

set -euo pipefail

if (($# != 4)); then
  printf 'usage: %s SOURCE_DIR INSTALLER DIST_DIR TAG\n' "$0" >&2
  exit 2
fi

source_dir="$(cd -- "$1" && pwd)"
installer="$(cd -- "$(dirname -- "$2")" && pwd)/$(basename -- "$2")"
dist_dir="$3"
tag="$4"

case "$tag" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *)
    printf 'invalid release tag: %s (expected vMAJOR.MINOR.PATCH)\n' "$tag" >&2
    exit 1
    ;;
esac

version="${tag#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  printf 'invalid stable release tag: %s\n' "$tag" >&2
  exit 1
}

for command_name in git go tar gzip sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$command_name" >&2
    exit 1
  }
done

[[ -f "$installer" ]] || {
  printf 'installer not found: %s\n' "$installer" >&2
  exit 1
}

head_commit="$(git -C "$source_dir" rev-parse HEAD)"
tag_commit="$(git -C "$source_dir" rev-list -n 1 "$tag")"
[[ "$head_commit" == "$tag_commit" ]] || {
  printf 'source checkout %s does not match %s (%s)\n' "$head_commit" "$tag" "$tag_commit" >&2
  exit 1
}
if [[ -n "$(git -C "$source_dir" status --porcelain --untracked-files=all)" ]]; then
  printf 'source checkout contains uncommitted or untracked files: %s\n' "$source_dir" >&2
  exit 1
fi

mapfile -t flake_versions < <(sed -n 's/^[[:space:]]*version = "\([^"]*\)";.*/\1/p; s/.*Version=\([0-9][0-9.]*\)".*/\1/p' "$source_dir/flake.nix")
[[ "${#flake_versions[@]}" -eq 2 ]] || {
  printf 'could not read both release versions from flake.nix\n' >&2
  exit 1
}
for declared_version in "${flake_versions[@]}"; do
  [[ "$declared_version" == "$version" ]] || {
    printf 'flake.nix declares %s but tag is %s\n' "$declared_version" "$tag" >&2
    exit 1
  }
done

aur_version="$(sed -n 's/^pkgver=//p' "$source_dir/packaging/aur/PKGBUILD")"
[[ "$aur_version" == "$version" ]] || {
  printf 'packaging/aur/PKGBUILD declares %s but tag is %s\n' "$aur_version" "$tag" >&2
  exit 1
}

mkdir -p -- "$dist_dir"
dist_dir="$(cd -- "$dist_dir" && pwd)"
if find "$dist_dir" -mindepth 1 -print -quit | grep -q .; then
  printf 'distribution directory is not empty: %s\n' "$dist_dir" >&2
  exit 1
fi

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

placeholder_count="$(grep -o '@SYNK_VERSION@' "$installer" | wc -l)"
[[ "$placeholder_count" -eq 1 ]] || {
  printf 'expected exactly one version placeholder in %s, found %s\n' "$installer" "$placeholder_count" >&2
  exit 1
}
release_installer="$work_dir/install.sh"
sed "s/@SYNK_VERSION@/$version/" "$installer" >"$release_installer"
chmod 0755 "$release_installer"

source_date_epoch="$(git -C "$source_dir" show -s --format=%ct "$tag^{commit}")"
ldflags="-s -w -X synk/internal/cli.Version=$version"

targets=(
  'linux amd64 linux-x86_64'
  'linux arm64 linux-arm64'
  'darwin amd64 macos-x86_64'
  'darwin arm64 macos-arm64'
)

for target in "${targets[@]}"; do
  read -r goos goarch platform <<<"$target"
  archive_root="synk-$version-$platform"
  stage_dir="$work_dir/$archive_root"
  mkdir -p -- "$stage_dir"

  (
    cd -- "$source_dir"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -buildvcs=false -ldflags "$ldflags" \
      -o "$stage_dir/synk" ./cmd/synk
  )

  install -m 0755 "$release_installer" "$stage_dir/install.sh"
  install -m 0644 "$source_dir/LICENSE" "$stage_dir/LICENSE"
  install -m 0644 "$source_dir/README.md" "$stage_dir/README.md"

  tar \
    --sort=name \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    --mtime="@$source_date_epoch" \
    -C "$work_dir" \
    -czf "$dist_dir/$archive_root.tar.gz" \
    "$archive_root"
done

git -C "$source_dir" archive \
  --format=tar \
  --prefix="synk-$version/" \
  "$tag" \
  | gzip -n -9 >"$dist_dir/synk-$version.tar.gz"

install -m 0755 "$release_installer" "$dist_dir/install.sh"

(
  cd -- "$dist_dir"
  sha256sum ./*.tar.gz ./install.sh | sed 's|  \./|  |' >SHA256SUMS
)

printf 'release artifacts written to %s\n' "$dist_dir"
