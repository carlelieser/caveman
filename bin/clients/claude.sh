# The claude CLI, pointed at the proxy.

client_launch() {
  command -v claude >/dev/null 2>&1 ||
    die "$EXIT_NO_CLIENT" "launching claude failed: not found on PATH"

  # exec so claude owns the TTY and receives signals directly, and its exit code
  # becomes ours.
  exec env \
    ANTHROPIC_BASE_URL="$CAVEMAN_BASE_URL" \
    ANTHROPIC_CUSTOM_HEADERS="X-Caveman-Compress: caveman" \
    ENABLE_TOOL_SEARCH=true \
    claude "$@"
}
