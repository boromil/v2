#!/bin/sh
# install.sh — Install OpenRC scripts for Miniflux on Alpine Linux
# Usage: sh install.sh [--sqlite-dir DIR] [--no-enable] [--no-backup] [--no-build] [--no-admin] [--admin-user USER] [--admin-pass PASS]

set -e

# Portable replacement for OpenRC's checkpath (which isn't in PATH when this
# script runs as plain `sh install.sh`). Uses only mkdir/chmod/chown/touch, which
# behave identically on busybox, util-linux, and BSD.
make_dir() {
  # $1=mode $2=owner $3=path
  mkdir -p "$3"
  chmod "$1" "$3"
  chown "$2" "$3"
}
make_file() {
  # $1=mode $2=owner $3=path  (creates empty file if missing; preserves existing)
  [ -f "$3" ] || : > "$3"
  chmod "$1" "$3"
  chown "$2" "$3"
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OPENRC_DIR="$(cd "$SCRIPT_DIR" && pwd)"
REPO_DIR="$(cd "$OPENRC_DIR/../../" && pwd)"

# Defaults
SQLITE_DIR="/var/lib/miniflux"
ENABLE_SERVICE=1
CONF_BACKUP=1
BUILD_BINARY=1
CREATE_ADMIN=0
ADMIN_USERNAME=""
ADMIN_PASSWORD=""
SKIP_ADMIN=0

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    --sqlite-dir)    SQLITE_DIR="$2"; shift 2 ;;
    --no-enable)     ENABLE_SERVICE=0; shift ;;
    --no-backup)     CONF_BACKUP=0; shift ;;
    --no-build)      BUILD_BINARY=0; shift ;;
    --no-admin)      SKIP_ADMIN=1; shift ;;
    --admin-user)    ADMIN_USERNAME="$2"; shift 2 ;;
    --admin-pass)    ADMIN_PASSWORD="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# Check root
if [ "$(id -u)" -ne 0 ]; then
  echo "Error: this script must be run as root (use sudo)."
  exit 1
fi

echo "=== Miniflux OpenRC Installer ==="
echo ""

# 1. Pre-install: create user/group
echo "[*] Creating miniflux user and group..."
sh "$OPENRC_DIR/miniflux.pre-install"

# 1.5. Build binary from source (if not already installed and --no-build not specified).
# Run in a subshell so the build's `cd` doesn't leak into the rest of the script.
if [ "$BUILD_BINARY" -eq 1 ] && [ ! -f /usr/bin/miniflux ]; then
  echo "[*] Building miniflux binary from source (SQLite support included)..."
  if ! command -v go >/dev/null 2>&1; then
    echo "Error: go is not installed. Install Go first, or use --no-build."
    exit 1
  fi
  if ! command -v make >/dev/null 2>&1; then
    echo "Error: make is not installed. Install make first, or use --no-build."
    exit 1
  fi
  (
    cd "$REPO_DIR" || exit 1
    make miniflux
    cp miniflux /usr/bin/miniflux
    chmod 0755 /usr/bin/miniflux
  )
fi

# 2. Create data directory
echo "[*] Creating SQLite data directory: $SQLITE_DIR"
make_dir 0755 miniflux:miniflux "$SQLITE_DIR"

# 3. Install init script
echo "[*] Installing init script to /etc/init.d/miniflux"
cp "$OPENRC_DIR/miniflux" /etc/init.d/miniflux
chmod 0755 /etc/init.d/miniflux

# 4. Install OpenRC conf.d
echo "[*] Installing OpenRC config to /etc/conf.d/miniflux"
cp "$OPENRC_DIR/miniflux.conf" /etc/conf.d/miniflux
chmod 0644 /etc/conf.d/miniflux

# 5. Install application config (with optional backup).
# The config file has NO admin credentials — they are created via the CLI after
# the service starts.  Mode 0644 is sufficient since the file holds no secrets.
echo "[*] Installing application config to /etc/miniflux.conf"
if [ -f /etc/miniflux.conf ] && [ "$CONF_BACKUP" -eq 1 ]; then
  cp /etc/miniflux.conf /etc/miniflux.conf.bak
  echo "    Existing config backed up to /etc/miniflux.conf.bak"
fi
cp "$OPENRC_DIR/miniflux.conf.application" /etc/miniflux.conf
chmod 0644 /etc/miniflux.conf

# 5.5. Admin user creation.
# Three paths:
#   (a) --admin-user AND --admin-pass on CLI  -> non-interactive, create admin
#   (b) only one of them on CLI               -> error (incomplete)
#   (c) neither on CLI                         -> interactive prompt (or skip)
if [ "$SKIP_ADMIN" -eq 1 ]; then
  CREATE_ADMIN=0
elif [ -n "$ADMIN_USERNAME" ] && [ -n "$ADMIN_PASSWORD" ]; then
  CREATE_ADMIN=1
elif [ -n "$ADMIN_USERNAME" ] || [ -n "$ADMIN_PASSWORD" ]; then
  echo "Error: --admin-user and --admin-pass must be provided together (or both omitted for interactive mode)."
  exit 1
else
  # Interactive prompt — requires a TTY.
  if ! [ -t 0 ]; then
    echo "Warning: no TTY on stdin; skipping admin creation."
    echo "  To create an admin non-interactively, re-run with --admin-user/--admin-pass."
    echo "  To create one interactively later, run from a real terminal:"
    echo "    su -s /bin/sh miniflux -c '/usr/bin/miniflux --config-file /etc/miniflux.conf --create-admin'"
    CREATE_ADMIN=0
  else
    echo ""
    echo "Admin user setup:"
    echo "  (leave both empty to skip admin creation)"
    echo ""

    # POSIX: no read -p. Don't default username here so we can detect skip.
    printf 'Admin username [admin]: '
    read -r ADMIN_USERNAME

    printf 'Admin password: '
    stty -echo 2>/dev/null || true
    read -r ADMIN_PASSWORD
    stty echo 2>/dev/null || true
    printf '\n'

    if [ -z "$ADMIN_USERNAME" ] && [ -z "$ADMIN_PASSWORD" ]; then
      CREATE_ADMIN=0
    else
      CREATE_ADMIN=1
      ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
    fi
  fi
fi

# Validate username charset early (mirrors miniflux's validator/user.go: [a-zA-Z0-9_@.-]).
# Fails fast with a clear message instead of a confusing miniflux error later.
if [ "$CREATE_ADMIN" -eq 1 ] && [ -n "$ADMIN_USERNAME" ]; then
  if ! printf '%s' "$ADMIN_USERNAME" | grep -Eq '^[A-Za-z0-9_@.-]+$'; then
    echo "Error: admin username contains invalid characters. Allowed: letters, digits, _ @ . -"
    exit 1
  fi
fi

# 6. Install logrotate config
echo "[*] Installing logrotate config to /etc/logrotate.d/miniflux"
cp "$OPENRC_DIR/logrotate-miniflux" /etc/logrotate.d/miniflux
chmod 0644 /etc/logrotate.d/miniflux

# 7. Initialize database file
echo "[*] Initializing database file at $SQLITE_DIR/miniflux.db"
make_file 0644 miniflux:miniflux "$SQLITE_DIR/miniflux.db"

# 8. Enable and start the service (optional)
if [ "$ENABLE_SERVICE" -eq 1 ]; then
  echo "[*] Enabling miniflux service (starts on boot)..."
  rc-update add miniflux default 2>/dev/null || true
fi

# 9. Start the service and finalize admin creation
if [ "$ENABLE_SERVICE" -eq 1 ]; then
  echo "[*] Starting miniflux service..."
  service miniflux start

  # Wait for the service to be ready (probe /healthcheck)
  echo "[*] Waiting for miniflux to be ready..."
  MAX_WAIT=30
  WAITED=0
  READY=0
  while [ "$WAITED" -lt "$MAX_WAIT" ]; do
    if wget -q -O- http://127.0.0.1:8080/healthcheck >/dev/null 2>&1; then
      READY=1
      break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
  done

  if [ "$READY" -ne 1 ]; then
    echo "Warning: miniflux did not become ready within ${MAX_WAIT}s."
    echo "  Check status: service miniflux status"
    echo "  View logs: tail -f /var/log/miniflux.log"
  fi

  # Admin creation — credentials never touch the config file.
  #
  # miniflux has no one-shot env-var admin flag: CREATE_ADMIN=1 makes it create
  # the admin AND start the daemon (cli.go falls through to startDaemon at L269).
  # The --create-admin flag IS one-shot but requires a TTY (ask_credentials.go).
  #
  # So:
  #   interactive      -> miniflux --create-admin (runs as root, inherits TTY)
  #   non-interactive  -> launch miniflux with CREATE_ADMIN=1 env, timeout after
  #                       admin creation succeeds (poll log), then kill it
  #                       (runs as miniflux user for correct file ownership).
  if [ "$READY" -eq 1 ] && [ "$CREATE_ADMIN" -eq 1 ]; then
    # Stop the OpenRC daemon first; the one-shot needs exclusive DB access.
    service miniflux stop 2>/dev/null || true

    if [ -n "$ADMIN_USERNAME" ] && [ -n "$ADMIN_PASSWORD" ]; then
      echo "[*] Creating admin user: $ADMIN_USERNAME (non-interactive)..."

      # Pass credentials as env-var prefixes to the miniflux command. This is
      # POSIX sh: `VAR=val cmd` sets the var in cmd's environment only, without
      # putting it in argv (so it doesn't show in `ps`). The password lives in
      # install.sh's shell variable and miniflux's process env briefly; it is
      # never written to disk.
      #
      # su's default is a cleaned env (doesn't preserve parent's env unless -p),
      # so the inline VAR=val prefix is the only path credentials take into
      # miniflux. timeout caps runtime: miniflux would otherwise fall through
      # to startDaemon.
      su -s /bin/sh miniflux -c \
        "CREATE_ADMIN=1 ADMIN_USERNAME='$ADMIN_USERNAME' ADMIN_PASSWORD='$ADMIN_PASSWORD' \
         timeout 15 /usr/bin/miniflux --config-file /etc/miniflux.conf" \
        >/var/log/miniflux-admin.log 2>&1 &
      _admin_pid=$!

      # Wait for admin creation (log line) or process exit / timeout.
      _wait=0
      _admin_ok=0
      while [ "$_wait" -lt 20 ]; do
        if grep -q 'Created new admin user' /var/log/miniflux-admin.log 2>/dev/null; then
          _admin_ok=1
          break
        fi
        if ! kill -0 "$_admin_pid" 2>/dev/null; then
          break
        fi
        sleep 1
        _wait=$((_wait + 1))
      done
      kill "$_admin_pid" 2>/dev/null || true
      wait "$_admin_pid" 2>/dev/null || true

      if [ "$_admin_ok" -ne 1 ]; then
        echo "Error: admin creation failed. Last 20 lines of miniflux output:"
        echo "---"
        tail -n 20 /var/log/miniflux-admin.log 2>/dev/null || echo "(no log file)"
        echo "---"
        echo "Full log: /var/log/miniflux-admin.log"
      fi
    else
      echo "[*] Creating admin user (interactive)..."
      # Run as the miniflux user so SQLite files keep correct ownership.
      # su inherits the parent's TTY by default (does not break term.IsTerminal);
      # the no-TTY case is already handled by the [ -t 0 ] guard above.
      # The if/else consumes the exit code, so set -e won't abort the install.
      if su -s /bin/sh miniflux -c \
        "/usr/bin/miniflux --config-file /etc/miniflux.conf --create-admin"; then
        _admin_ok=1
      else
        _admin_ok=0
        echo "Error: interactive admin creation failed."
      fi
    fi

    echo "[*] Restarting miniflux service..."
    service miniflux start
  fi
fi

echo ""
echo "=== Installation complete ==="
echo ""
if [ "$ENABLE_SERVICE" -eq 1 ]; then
  echo "Next steps:"
  if [ "$CREATE_ADMIN" -eq 1 ] && [ -n "$ADMIN_USERNAME" ] && [ "${_admin_ok:-0}" -eq 1 ]; then
    echo "  1. Admin user created: $ADMIN_USERNAME"
  elif [ "$CREATE_ADMIN" -eq 1 ]; then
    echo "  1. Admin user creation FAILED. See /var/log/miniflux-admin.log"
    echo "     To retry: su -s /bin/sh miniflux -c '/usr/bin/miniflux --config-file /etc/miniflux.conf --create-admin'"
  else
    echo "  1. No admin user was created. Create one interactively with:"
    echo "       su -s /bin/sh miniflux -c '/usr/bin/miniflux --config-file /etc/miniflux.conf --create-admin'"
    echo "     (requires a TTY — for automation, re-run install.sh with --admin-user/--admin-pass)"
  fi
  echo "  2. Edit /etc/miniflux.conf if you need custom settings (e.g., LISTEN_ADDR)"
  echo "  3. Check status: service miniflux status"
  echo "  4. View logs: tail -f /var/log/miniflux.log"
else
  echo "Service is NOT enabled. Enable it with: rc-update add miniflux default"
  echo "Start it with: service miniflux start"
fi
