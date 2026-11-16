#!/bin/sh
# Build an owner-preview package from exactly one committed source snapshot.
set -eu
umask 022
export LC_ALL=C TZ=UTC
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$repo"
[ "$(uname -s)" = Linux ] || { echo 'Build on Debian 12 amd64 with the documented toolchain.' >&2; exit 1; }
[ "$(dpkg --print-architecture)" = amd64 ] || { echo 'Native Debian amd64 build is required.' >&2; exit 1; }
export GOTOOLCHAIN=local
[ "$(go env GOVERSION)" = go1.27.1 ] || { echo 'Go 1.27.1 is required.' >&2; exit 1; }
git diff --quiet && git diff --cached --quiet || { echo 'Commit or stash tracked changes before building.' >&2; exit 1; }
commit=$(git rev-parse --verify 'HEAD^{commit}')
epoch=$(git show -s --format=%ct "$commit")
case "$commit" in *[!0-9a-f]*|'') echo 'Invalid Git commit.' >&2; exit 1;; esac
case "$epoch" in *[!0-9]*|'') echo 'Invalid Git timestamp.' >&2; exit 1;; esac
output=${1:-"$repo/build/deb-preview"}
mkdir -p -- "$output"
output=$(CDPATH= cd -- "$output" && pwd -P)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/meridian-deb.XXXXXXXX")
[ -n "$tmp" ] && [ -d "$tmp" ] || { echo 'Cannot create temporary build directory.' >&2; exit 1; }
cleanup() { case "$tmp" in "${TMPDIR:-/tmp}"/meridian-deb.*) rm -rf -- "$tmp";; *) echo 'Unexpected temporary path; manual cleanup required.' >&2;; esac; }
trap cleanup EXIT HUP INT TERM
mkdir "$tmp/src"
git archive --format=tar --output="$tmp/source.tar" "$commit"
tar -xf "$tmp/source.tar" -C "$tmp/src"
cp -R "$tmp/src/packaging/debian-draft/debian" "$tmp/src/debian"
cd "$tmp/src"
export MERIDIAN_BUILD_COMMIT="$commit" SOURCE_DATE_EPOCH="$epoch" GOTOOLCHAIN=local
# Debian records shared-library dependencies from the actual UI executable.
dpkg-buildpackage -b --no-sign
set -- "$tmp"/meridian-vpn_*_amd64.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo 'Expected one amd64 package.' >&2; exit 1; }
cp -- "$1" "$output/"
sha256sum "$output/$(basename -- "$1")"
