# The claude CLI, pointed at the proxy.

client_launch() {
  command -v claude >/dev/null 2>&1 ||
    die "$EXIT_NO_CLIENT" "launching claude failed: not found on PATH"

  # `off` sends no Caveman header at all, so the request forwards byte-identical
  # rather than relying on the server to parse a level that means "do nothing".
  headers=''
  if [ "$CAVEMAN_LEVEL" != off ]; then
    headers="X-Caveman-Compress: $CAVEMAN_LEVEL"
  fi

  # exec so claude owns the TTY and receives signals directly, and its exit code
  # becomes ours.
  exec env \
    ANTHROPIC_BASE_URL="$CAVEMAN_BASE_URL" \
    ANTHROPIC_CUSTOM_HEADERS="$headers" \
    ENABLE_TOOL_SEARCH=true \
    claude "$@"
}
