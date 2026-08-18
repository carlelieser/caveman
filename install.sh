#!/usr/bin/env bash
# Installs the caveman CLI. Safe to re-run: an existing binary is replaced.
#
#   curl -fsSL https://raw.githubusercontent.com/carlelieser/caveman/main/install.sh | bash
#
# Run from inside a clone, that clone's own build is used, so edits are live.
set -euo pipefail

REPOSITORY=carlelieser/caveman
BIN_DIR=$HOME/.local/bin
VERSION=${CAVEMAN_VERSION:-latest}

abort() {
  printf 'install: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || abort "$1 is required but not on PATH"
}

# Piped through bash, $0 is "bash" and the script cannot locate itself, so a
# clone is detected from cwd instead.
is_caveman_clone() {
  [ -f "$1/go.mod" ] || return 1
  grep -q '^module github.com/carlelieser/caveman$' "$1/go.mod" 2>/dev/null
}

# asset_name maps the running platform onto a release asset. An unsupported
# pair fails here rather than downloading something that cannot run.
asset_name() {
  os=$(uname -s)
  arch=$(uname -m)

  case $os in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) abort "no release build for $os; build from source with: go build ./cmd/caveman" ;;
  esac

  case $arch in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) abort "no release build for $arch; build from source with: go build ./cmd/caveman" ;;
  esac

  printf 'caveman_%s_%s.tar.gz' "$os" "$arch"
}

download_url() {
  asset=$1
  if [ "$VERSION" = latest ]; then
    printf 'https://github.com/%s/releases/latest/download/%s' "$REPOSITORY" "$asset"
  else
    printf 'https://github.com/%s/releases/download/%s/%s' "$REPOSITORY" "$VERSION" "$asset"
  fi
}

install_binary() {
  source_path=$1
  mkdir -p "$BIN_DIR" || abort "creating $BIN_DIR failed"
  chmod +x "$source_path" || abort "making the binary executable failed"
  # Moved into place rather than copied over, so a running caveman keeps the
  # inode it started from and the replacement is atomic.
  mv -f "$source_path" "$BIN_DIR/caveman" || abort "installing into $BIN_DIR failed"
}

install_from_release() {
  require_command curl
  require_command tar
  require_command uname

  asset=$(asset_name)
  url=$(download_url "$asset")
  workspace=$(mktemp -d) || abort "creating a temporary directory failed"
  trap 'rm -rf "$workspace"' EXIT

  printf 'install: fetching %s\n' "$url"
  curl -fsSL "$url" -o "$workspace/$asset" ||
    abort "downloading $url failed; check that a release for this platform exists"
  tar -xzf "$workspace/$asset" -C "$workspace" || abort "extracting $asset failed"

  binary=$workspace/caveman
  [ -f "$binary" ] || binary=$(find "$workspace" -type f -name caveman -print -quit)
  [ -n "$binary" ] && [ -f "$binary" ] || abort "$asset does not carry a caveman binary"

  install_binary "$binary"
}

install_from_clone() {
  root=$1
  require_command go
  printf 'install: building %s\n' "$root"
  workspace=$(mktemp -d) || abort "creating a temporary directory failed"
  trap 'rm -rf "$workspace"' EXIT
  (cd "$root" && go build -o "$workspace/caveman" ./cmd/caveman) ||
    abort "building $root failed"
  install_binary "$workspace/caveman"
}

report_path() {
  case ":$PATH:" in
    *":$BIN_DIR:"*) return 0 ;;
  esac
  printf '\nAdd %s to your PATH:\n' "$BIN_DIR"
  case ${SHELL:-} in
    *zsh) printf '  echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc\n' "$BIN_DIR" ;;
    *) printf '  export PATH="%s:$PATH"\n' "$BIN_DIR" ;;
  esac
}

main() {
  if is_caveman_clone "$PWD"; then
    install_from_clone "$PWD"
  else
    install_from_release
  fi

  printf 'install: installed %s/caveman\n' "$BIN_DIR"
  report_path
  printf '\nRun: caveman claude\n'
}

main "$@"
