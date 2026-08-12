#!/bin/sh
# A stand-in for systemctl, because systemd does not run in this container.
#
# It drives the unbound process directly and answers the four things the tool
# asks: is-active, reload, restart, and whether the unit can be reloaded. That
# keeps both systemctl-based refresh tiers on their real code paths, so a test
# exercises the same commands a production server would receive.
#
# It is deliberately not a general systemctl: anything else is refused loudly
# rather than silently succeeding.
set -eu

CONFIG=/etc/unbound/unbound.conf
PIDFILE=/run/unbound.pid

# Liveness is read from /proc rather than with kill -0, because unbound runs as
# root here and kill -0 would report a live process as dead for any other user.
running_pid() {
    [ -f "$PIDFILE" ] || return 1

    pid=$(cat "$PIDFILE" 2>/dev/null) || return 1
    [ -n "$pid" ] || return 1

    [ -d "/proc/$pid" ] || return 1

    printf '%s' "$pid"
}

# wait_active polls for the daemon to come up, because unbound forks and the
# pidfile appears a moment after the parent exits.
wait_active() {
    i=0
    while [ "$i" -lt 50 ]; do
        if running_pid >/dev/null; then
            return 0
        fi

        i=$((i + 1))
        sleep 0.1
    done

    return 1
}

start_unbound() {
    unbound -c "$CONFIG"
    wait_active
}

stop_unbound() {
    pid=$(running_pid) || return 0

    kill -TERM "$pid" 2>/dev/null || true

    i=0
    while [ "$i" -lt 50 ]; do
        [ -d "/proc/$pid" ] || return 0

        i=$((i + 1))
        sleep 0.1
    done

    kill -KILL "$pid" 2>/dev/null || true
    return 0
}

action=${1:-}
shift || true

case "$action" in
is-active)
    if running_pid >/dev/null; then
        echo active
        exit 0
    fi

    echo inactive
    # The exit code systemd uses for an inactive unit.
    exit 3
    ;;

reload)
    pid=$(running_pid) || {
        echo "unbound çalışmıyor" >&2
        exit 1
    }

    # SIGHUP makes unbound re-read its configuration. A config it rejects on
    # re-read kills the process, which is exactly why the tool checks
    # is-active afterwards instead of trusting this exit code.
    kill -HUP "$pid"
    ;;

restart)
    stop_unbound
    start_unbound || {
        echo "unbound başlatılamadı" >&2
        exit 1
    }
    ;;

start)
    running_pid >/dev/null && exit 0
    start_unbound || exit 1
    ;;

stop)
    stop_unbound
    ;;

show)
    # Only the CanReload query is answered, which is what decides whether the
    # signal reload tier is offered.
    for arg in "$@"; do
        case "$arg" in
        --property=CanReload)
            echo yes
            exit 0
            ;;
        esac
    done

    echo ""
    ;;

*)
    echo "systemctl kalıbı $action işlemini desteklemiyor" >&2
    exit 1
    ;;
esac
