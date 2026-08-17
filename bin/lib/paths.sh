# Install root, run directory, and the files under it.

# Takes the already-resolved bin directory: the entry script has to resolve its
# own symlink before it can source this file.
init_paths() {
  if [ -z "${CAVEMAN_HOME:-}" ]; then
    CAVEMAN_HOME=$(cd "$1/.." && pwd -P) ||
      die "$EXIT_FAILURE" "resolving the install root from $1 failed"
  fi
  [ -f "$CAVEMAN_HOME/package.json" ] ||
    die "$EXIT_FAILURE" "reading install root $CAVEMAN_HOME failed: no package.json"

  CAVEMAN_RUN_DIR=${CAVEMAN_RUN_DIR:-$CAVEMAN_HOME/run}
  CAVEMAN_PID_FILE=$CAVEMAN_RUN_DIR/caveman.pid
  CAVEMAN_LOG_FILE=$CAVEMAN_RUN_DIR/caveman.log
  CAVEMAN_CLIENT_DIR=${CAVEMAN_CLIENT_DIR:-$CAVEMAN_HOME/bin/clients}
}

ensure_run_dir() {
  mkdir -p "$CAVEMAN_RUN_DIR" ||
    die "$EXIT_FAILURE" "creating run directory $CAVEMAN_RUN_DIR failed"
}
