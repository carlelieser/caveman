# Resolving and launching a client from the client directory.

client_script() {
  printf '%s/%s.sh' "$CAVEMAN_CLIENT_DIR" "$1"
}

has_client() {
  [ -f "$(client_script "$1")" ]
}

# Names come from the directory listing, so help can never drift from what is
# actually installed.
client_names() {
  [ -d "$CAVEMAN_CLIENT_DIR" ] || return 0
  for script in "$CAVEMAN_CLIENT_DIR"/*.sh; do
    [ -f "$script" ] || continue
    name=$(basename "$script")
    printf '%s\n' "${name%.sh}"
  done
}

# Starts the server if needed, then hands the process to the client. Quiet when
# the server is already up, since this runs before every client launch.
launch_client() {
  name=$1
  shift
  if ! is_running; then
    start_server
  fi
  # shellcheck source=/dev/null
  . "$(client_script "$name")"
  client_launch "$@"
}
