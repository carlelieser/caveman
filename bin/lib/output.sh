# Messages and exit codes. Sourced, never executed.

EXIT_OK=0
EXIT_FAILURE=1
EXIT_USAGE=2
EXIT_PORT_TAKEN=3
EXIT_NOT_READY=4
EXIT_NO_CLIENT=127

say() {
  printf '%s\n' "$*"
}

warn() {
  printf '%s\n' "$*" >&2
}

# Names the operation that failed and exits with the given code.
die() {
  code=$1
  shift
  warn "caveman: $*"
  exit "$code"
}
