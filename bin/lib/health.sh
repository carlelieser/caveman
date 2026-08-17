# Liveness probing and the readiness loop.

READY_TIMEOUT_SECONDS=15
PROBE_INTERVAL_SECONDS=0.2
PROBE_TIMEOUT_SECONDS=2
LOG_TAIL_LINES=20

# `caveman` if our server answers, `foreign` if something else holds the port,
# `down` if nothing answers. The service name is what separates the first two:
# a 200 alone proves only that the port is taken.
probe_health() {
  if body=$(curl -fsS --max-time "$PROBE_TIMEOUT_SECONDS" \
    "$CAVEMAN_BASE_URL/health" 2>/dev/null); then
    case $body in
      *'"service":"caveman"'*) printf 'caveman' ;;
      *) printf 'foreign' ;;
    esac
    return 0
  fi
  # curl failed: either nothing is listening, or something answered non-2xx.
  if curl -s --max-time "$PROBE_TIMEOUT_SECONDS" -o /dev/null \
    "$CAVEMAN_BASE_URL/health" 2>/dev/null; then
    printf 'foreign'
  else
    printf 'down'
  fi
}

is_running() {
  [ "$(probe_health)" = 'caveman' ]
}

print_log_tail() {
  [ -f "$CAVEMAN_LOG_FILE" ] || return 0
  warn "--- last $LOG_TAIL_LINES lines of $CAVEMAN_LOG_FILE ---"
  tail -n "$LOG_TAIL_LINES" "$CAVEMAN_LOG_FILE" >&2
}

# Waits for the launched process to answer. Checks liveness before probing, so a
# server that dies on boot reports the crash instead of burning the full budget.
await_ready() {
  pid=$1
  deadline=$(( $(date +%s) + READY_TIMEOUT_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      warn "caveman: the server exited during startup"
      print_log_tail
      return "$EXIT_NOT_READY"
    fi
    case $(probe_health) in
      caveman) return "$EXIT_OK" ;;
      foreign) return "$EXIT_PORT_TAKEN" ;;
    esac
    sleep "$PROBE_INTERVAL_SECONDS"
  done
  warn "caveman: the server did not answer on port $CAVEMAN_PORT within ${READY_TIMEOUT_SECONDS}s"
  print_log_tail
  return "$EXIT_NOT_READY"
}
