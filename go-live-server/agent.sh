#!/usr/bin/env bash
# agent.sh — run on the Raspberry Pi
# Polls the Go server for tasks and executes them.
set -euo pipefail

# ---- config ----
SERVER="${AGENT_SERVER:-10.95.133.96:9090}"
AGENT_ID="${AGENT_ID:-pi-cam-01}"
POLL_SEC="${POLL_SEC:-5}"
FFMPEG_OPTS="${FFMPEG_OPTS:--c:v h264_omx -b:v 2000k -preset ultrafast}"
VIDEO_DEV="${VIDEO_DEV:-/dev/video0}"

unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY

log()  { echo "[$(date +%H:%M:%S)] $*"; }
warn() { echo "[$(date +%H:%M:%S)] WARN: $*"; }

# ---- state ----
CURRENT_TASK_ID=""
CURRENT_FFMPEG_PID=""

cleanup() {
    if [ -n "$CURRENT_FFMPEG_PID" ] && kill -0 "$CURRENT_FFMPEG_PID" 2>/dev/null; then
        log "stopping ffmpeg (pid=$CURRENT_FFMPEG_PID)..."
        kill "$CURRENT_FFMPEG_PID" 2>/dev/null || true
        wait "$CURRENT_FFMPEG_PID" 2>/dev/null || true
    fi
    log "agent stopped."
}
trap cleanup EXIT INT TERM

# Report task result back to server
report() {
    local task_id="$1" status="$2" err_msg="${3:-}"
    log "reporting task $task_id → $status"
    curl -s -X PUT "${SERVER}/api/agent/tasks/${task_id}" \
        -H "Content-Type: application/json" \
        -d "{\"status\":\"${status}\",\"error_msg\":\"${err_msg}\"}" >/dev/null
}

# Start ffmpeg for a start_push task
do_start_push() {
    local task_id="$1" push_url="$2" stream_key="$3"

    log "start_push: stream=$stream_key"
    log "  push_url=$push_url"

    # check video device
    if [ ! -e "$VIDEO_DEV" ]; then
        report "$task_id" "failed" "video device $VIDEO_DEV not found"
        return
    fi

    # launch ffmpeg
    ffmpeg -re -i "$VIDEO_DEV" $FFMPEG_OPTS -f flv "$push_url" &
    CURRENT_FFMPEG_PID=$!
    CURRENT_TASK_ID="$task_id"

    log "ffmpeg started (pid=$CURRENT_FFMPEG_PID)"

    # wait for ffmpeg to exit, then report
    wait "$CURRENT_FFMPEG_PID" 2>/dev/null || true
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        report "$task_id" "completed"
    else
        report "$task_id" "failed" "ffmpeg exited with code $exit_code"
    fi

    CURRENT_FFMPEG_PID=""
    CURRENT_TASK_ID=""
}

# Kill running ffmpeg for a stop_push task
do_stop_push() {
    local task_id="$1"
    log "stop_push"

    if [ -n "$CURRENT_FFMPEG_PID" ] && kill -0 "$CURRENT_FFMPEG_PID" 2>/dev/null; then
        kill "$CURRENT_FFMPEG_PID" 2>/dev/null || true
        wait "$CURRENT_FFMPEG_PID" 2>/dev/null || true
        report "$task_id" "completed"
    else
        report "$task_id" "completed"  # nothing to stop
    fi
    CURRENT_FFMPEG_PID=""
    CURRENT_TASK_ID=""
}

# ---- main loop ----
log "agent starting — server=$SERVER agent=$AGENT_ID"

while true; do
    # Only poll if not currently streaming
    if [ -z "$CURRENT_FFMPEG_PID" ] || ! kill -0 "$CURRENT_FFMPEG_PID" 2>/dev/null; then
        CURRENT_FFMPEG_PID=""
        CURRENT_TASK_ID=""
    fi

    # Skip polling if busy
    if [ -n "$CURRENT_FFMPEG_PID" ]; then
        sleep "$POLL_SEC"
        continue
    fi

    # Poll for tasks
    resp=$(curl -s "${SERVER}/api/agent/tasks?agent_id=${AGENT_ID}" 2>/dev/null) || {
        warn "server unreachable, retrying in ${POLL_SEC}s..."
        sleep "$POLL_SEC"
        continue
    }

    # Parse first task (jq or python3 fallback)
    task=$(echo "$resp" | python3 -c "
import sys,json
tasks = json.load(sys.stdin).get('data',{}).get('tasks',[])
if tasks:
    t = tasks[0]
    print(json.dumps({'id':t['id'],'action':t['action'],'push_url':t.get('push_url',''),'stream_key':t.get('stream_key','')}))
" 2>/dev/null) || task=""

    if [ -z "$task" ]; then
        sleep "$POLL_SEC"
        continue
    fi

    task_id=$(echo "$task" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
    action=$(echo "$task" | python3 -c "import sys,json; print(json.load(sys.stdin)['action'])")
    push_url=$(echo "$task" | python3 -c "import sys,json; print(json.load(sys.stdin)['push_url'])")
    stream_key=$(echo "$task" | python3 -c "import sys,json; print(json.load(sys.stdin)['stream_key'])")

    log "got task: action=$action id=${task_id:0:8}..."

    case "$action" in
        start_push) do_start_push "$task_id" "$push_url" "$stream_key" ;;
        stop_push)  do_stop_push  "$task_id" ;;
        *)
            warn "unknown action: $action"
            report "$task_id" "failed" "unknown action: $action"
            ;;
    esac

    sleep "$POLL_SEC"
done
