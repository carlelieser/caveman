#!/usr/bin/env bash
# Installs the caveman CLI. Safe to re-run: an existing clone is updated and an
# existing symlink is replaced.
#
#   curl -fsSL https://raw.githubusercontent.com/carlelieser/caveman/main/install.sh | bash
#
# Run from inside a clone, that clone is used in place, so edits are live.
set -euo pipefail

REPOSITORY_URL=https://github.com/carlelieser/caveman.git
DEFAULT_ROOT=$HOME/.caveman
BIN_DIR=$HOME/.local/bin

abort() {
  printf 'install: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || abort "$1 is required but not on PATH"
}

is_caveman_clone() {
  [ -f "$1/package.json" ] || return 1
  grep -q '"name": *"caveman"' "$1/package.json" 2>/dev/null
}

# Piped through bash, $0 is "bash" and the script cannot locate itself, so the
# clone is detected from cwd instead.
resolve_root() {
  if is_caveman_clone "$PWD"; then
    printf '%s' "$PWD"
    return 0
  fi
  if [ -d "$DEFAULT_ROOT/.git" ]; then
    git -C "$DEFAULT_ROOT" pull --ff-only >/dev/null 2>&1 ||
      printf 'install: could not update %s, using it as is\n' "$DEFAULT_ROOT" >&2
  else
    git clone --depth 1 "$REPOSITORY_URL" "$DEFAULT_ROOT" >/dev/null 2>&1 ||
      abort "cloning $REPOSITORY_URL into $DEFAULT_ROOT failed"
  fi
  printf '%s' "$DEFAULT_ROOT"
}

link_entry_script() {
  root=$1
  mkdir -p "$BIN_DIR" || abort "creating $BIN_DIR failed"
  # -f replaces a previous install, -n avoids linking inside an existing link.
  ln -sfn "$root/bin/caveman" "$BIN_DIR/caveman" ||
    abort "linking $BIN_DIR/caveman failed"
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
  require_command git
  require_command node
  require_command npm

  root=$(resolve_root)
  printf 'install: using %s\n' "$root"

  # Subshell because piping through bash leaves cwd wherever the caller was.
  # `npm install` rather than `npm ci`: ci deletes node_modules outright, which
  # would make reusing a working clone destructive.
  (cd "$root" && npm install --silent) || abort "npm install in $root failed"

  chmod +x "$root/bin/caveman" || abort "making $root/bin/caveman executable failed"
  link_entry_script "$root"

  printf 'install: linked %s/caveman\n' "$BIN_DIR"
  report_path
  printf '\nRun: caveman claude\n'
}

main "$@"
