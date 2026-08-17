# Starting, stopping, and reporting on the backgrounded server.

TERM_TIMEOUT_SECONDS=10
STOP_INTERVAL_SECONDS=0.1
# Matches the session total, not the per-request line that also says "session".
SUMMARY_PATTERN='^caveman  session  '

read_pid() {
  [ -f "$CAVEMAN_PID_FILE" ] || return 1
  pid=$(cat "$CAVEMAN_PID_FILE" 2>/dev/null) || return 1
  [ -n "$pid" ] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  printf '%s' "$pid"
}

clear_stale_pid() {
  read_pid >/dev/null || rm -f "$CAVEMAN_PID_FILE"
}

# Runs node directly rather than `npm start`: npm would sit between the CLI and
# the server, and a SIGTERM to npm does not reliably reach the child, which is
# what prints the session summary.
spawn_server() {
  ensure_run_dir
  printf -- '--- started %s ---\n' "$(date '+%Y-%m-%dT%H:%M:%S')" >>"$CAVEMAN_LOG_FILE"
  (
    cd "$CAVEMAN_HOME" &&
      exec nohup node --import tsx src/http/server.ts >>"$CAVEMAN_LOG_FILE" 2>&1
  ) &
  printf '%s' "$!" >"$CAVEMAN_PID_FILE"
  printf '%s' "$!"
}

start_server() {
  case $(probe_health) in
    caveman)
      say "caveman already running on $CAVEMAN_BASE_URL"
      return "$EXIT_OK"
      ;;
    foreign)
      die "$EXIT_PORT_TAKEN" \
        "port $CAVEMAN_PORT is held by another process; stop it or set PORT"
      ;;
  esac

  clear_stale_pid
  pid=$(spawn_server)

  status="$EXIT_OK"
  await_ready "$pid" || status=$?
  if [ "$status" -ne "$EXIT_OK" ]; then
    rm -f "$CAVEMAN_PID_FILE"
    exit "$status"
  fi
  say "caveman listening on $CAVEMAN_BASE_URL"
  say "logs: $CAVEMAN_LOG_FILE"
}

# The session summary lands in the log because stdout was redirected at launch,
# so it is echoed here rather than left where nobody looks.
echo_session_summary() {
  [ -f "$CAVEMAN_LOG_FILE" ] || return 0
  summary=$(tail -n 20 "$CAVEMAN_LOG_FILE" | grep "$SUMMARY_PATTERN" | tail -n 1) || true
  [ -n "$summary" ] && say "$summary"
  return 0
}

await_exit() {
  pid=$1
  deadline=$(( $(date +%s) + TERM_TIMEOUT_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep "$STOP_INTERVAL_SECONDS"
  done
  return 1
}

# SIGTERM, never SIGKILL first: the server prints the session summary on SIGTERM
# and a hard kill skips it.
stop_server() {
  if ! pid=$(read_pid); then
    rm -f "$CAVEMAN_PID_FILE"
    say "caveman is not running"
    return "$EXIT_OK"
  fi

  kill -TERM "$pid" 2>/dev/null || true
  if await_exit "$pid"; then
    say "caveman stopped"
    echo_session_summary
  else
    kill -KILL "$pid" 2>/dev/null || true
    warn "caveman: pid $pid ignored SIGTERM for ${TERM_TIMEOUT_SECONDS}s; killed (session summary lost)"
  fi
  rm -f "$CAVEMAN_PID_FILE"
}

report_status() {
  if is_running; then
    say "caveman is running on $CAVEMAN_BASE_URL"
    pid=$(read_pid) && say "pid: $pid"
    say "logs: $CAVEMAN_LOG_FILE"
    return "$EXIT_OK"
  fi
  say "caveman is not running (port $CAVEMAN_PORT)"
  return "$EXIT_FAILURE"
}
