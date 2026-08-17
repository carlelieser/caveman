# The compression level: parsing the flag, and remembering what `up` was given.

LEVEL_NAMES='off light moderate caveman'
DEFAULT_LEVEL=off

is_level() {
  for name in $LEVEL_NAMES; do
    [ "$1" = "$name" ] && return 0
  done
  return 1
}

require_level() {
  is_level "$1" ||
    die "$EXIT_USAGE" "reading --level failed: \"$1\" is not one of $LEVEL_NAMES"
}

# Strips --level/-l from the arguments and leaves the rest in CAVEMAN_ARGS, so a
# client receives only what belongs to it. Sets CAVEMAN_LEVEL when the flag is
# present, leaving it unset otherwise, which is what lets a stored level show
# through.
parse_level_flag() {
  CAVEMAN_ARGS=()
  while [ "$#" -gt 0 ]; do
    case $1 in
      --)
        # Everything after `--` belongs to the client, including its own -l.
        shift
        while [ "$#" -gt 0 ]; do
          CAVEMAN_ARGS+=("$1")
          shift
        done
        ;;
      --level | -l)
        [ "$#" -ge 2 ] || die "$EXIT_USAGE" "reading $1 failed: no level given"
        require_level "$2"
        CAVEMAN_LEVEL=$2
        shift 2
        ;;
      --level=*)
        value=${1#--level=}
        require_level "$value"
        CAVEMAN_LEVEL=$value
        shift
        ;;
      *)
        CAVEMAN_ARGS+=("$1")
        shift
        ;;
    esac
  done
}

read_stored_level() {
  [ -f "$CAVEMAN_LEVEL_FILE" ] || return 1
  stored=$(cat "$CAVEMAN_LEVEL_FILE" 2>/dev/null) || return 1
  is_level "$stored" || return 1
  printf '%s' "$stored"
}

store_level() {
  ensure_run_dir
  printf '%s' "$1" >"$CAVEMAN_LEVEL_FILE" ||
    die "$EXIT_FAILURE" "writing the level to $CAVEMAN_LEVEL_FILE failed"
}

# A level on this command wins; otherwise the one `up` stored; otherwise off.
resolve_level() {
  if [ -n "${CAVEMAN_LEVEL:-}" ]; then
    printf '%s' "$CAVEMAN_LEVEL"
    return 0
  fi
  read_stored_level || printf '%s' "$DEFAULT_LEVEL"
}
