# The port the server would bind.

PORT_QUERY='import("./src/http/server.ts").then((m)=>process.stdout.write(String(m.listenPort())))'

# Asks the server's own listenPort() rather than parsing .env here, so the CLI
# and the server can never disagree about precedence, quoting, or validity.
# Runs from the install root because dotenv resolves .env against cwd.
# DOTENV_CONFIG_QUIET keeps dotenv's banner off stdout, which would otherwise
# arrive as part of the value.
read_port() {
  if port=$(cd "$CAVEMAN_HOME" && DOTENV_CONFIG_QUIET=true node --import tsx \
    -e "$PORT_QUERY" 2>/dev/null); then
    printf '%s' "$port"
    return 0
  fi
  return 1
}

# Re-runs the query showing stderr, so the server's own message is what the user
# sees when the port is unreadable.
report_port_failure() {
  reason=$( (cd "$CAVEMAN_HOME" && DOTENV_CONFIG_QUIET=true node --import tsx \
    -e "$PORT_QUERY" 2>&1 >/dev/null) | grep -m 1 '^Error: ' | sed 's/^Error: //' || true)
  warn "caveman: reading the configured port failed"
  if [ -n "$reason" ]; then
    warn "  $reason"
  fi
  exit "$EXIT_FAILURE"
}

init_port() {
  if ! CAVEMAN_PORT=$(read_port); then
    report_port_failure
  fi
  CAVEMAN_BASE_URL="http://localhost:$CAVEMAN_PORT"
}
