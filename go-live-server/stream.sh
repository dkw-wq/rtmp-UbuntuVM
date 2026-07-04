#!/usr/bin/env bash
set -euo pipefail

# ---- config ----
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
SERVER="${STREAM_SERVER:-localhost:9090}"
USERNAME="${STREAM_USER:-admin}"
PASSWORD="${STREAM_PASS:-admin123}"
CMD="${1:-help}"

_login() {
    curl -s -X POST "${SERVER}/api/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}"
}
_token() { _login | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["token"])'; }

_py() { python3 -c "$1" "$2"; }

case "$CMD" in
    create)
        res="${2:-}"; br="${3:-}"
        body='{"protocol":"rtmp"'
        [ -n "$res" ] && body="${body},\"resolution\":\"${res}\""
        [ -n "$br" ]  && body="${body},\"bitrate\":\"${br}\""
        body="${body}}"

        resp=$(curl -s -X POST "${SERVER}/api/streams" \
            -H "Authorization: Bearer $(_token)" \
            -H "Content-Type: application/json" \
            -d "$body")

        _py '
import sys,json
d=json.loads(sys.argv[1])["data"]
print("Stream ID:  ", d["id"])
print("Stream Key: ", d["stream_key"])
print("Status:     ", d["status"])
print("Resolution: ", d.get("resolution","default"))
print("Bitrate:    ", d.get("bitrate","default"))
print("")
print("Push URL (Pi uses this):")
print("  " + d["push_url"])
print("")
print("Playback HLS (viewer opens in VLC):")
print("  " + d["hls_url"])
print("")
print("Playback HTTP-FLV:")
print("  " + d.get("flv_url", "N/A"))
print("")
print("Test push (on VM, no Pi):")
print("  ffmpeg -re -f lavfi -i testsrc=size=1280x720:rate=30 -c:v libx264 -preset ultrafast -b:v 2000k -f flv \"" + d["push_url"] + "\"")
' "$resp"
        ;;

    list)
        status="${2:-}"
        url="${SERVER}/api/streams"
        [ -n "$status" ] && url="${url}?status=${status}"
        resp=$(curl -s "$url" -H "Authorization: Bearer $(_token)")
        _py '
import sys,json
data=json.loads(sys.argv[1]).get("data",[])
if not data: print("No streams."); sys.exit(0)
print("%-10s %-12s %-12s %-12s %-10s" % ("ID","Status","Key","Res","BR"))
print("-"*58)
for s in data:
    print("%-10s %-12s %-12s %-12s %-10s" % (
        s["id"][:8], s["status"], s["stream_key"][:10],
        s.get("resolution","-"), s.get("bitrate","-")))
' "$resp"
        ;;

    delete)
        id="${2:?Usage: stream.sh delete <stream_id>}"
        resp=$(curl -s -X DELETE "${SERVER}/api/streams/${id}" -H "Authorization: Bearer $(_token)")
        _py 'import sys,json; print(json.dumps(json.loads(sys.argv[1]),indent=2))' "$resp"
        ;;

    playback)
        key="${2:?Usage: stream.sh playback <stream_key>}"
        resp=$(curl -s "${SERVER}/api/playback/${key}")
        _py '
import sys,json
d=json.loads(sys.argv[1])["data"]
print("Status:", d["status"])
print("HLS:   ", d["hls_url"])
print("FLV:   ", d["flv_url"])
' "$resp"
        ;;

    help|*)
        cat <<'HELP'
stream.sh — one-command stream management

  create [res] [br]    Create stream and print direct Pi push URL
  list [status]        List streams
  delete <id>          Delete a stream
  playback <key>       Get viewer playback URL

Examples:
  stream.sh create                     default rtmp
  stream.sh create 1920x1080 4000k     full HD
  stream.sh list publishing            only live streams
  stream.sh playback abc-def-123       get URL for VLC

Remote VM:
  STREAM_SERVER=10.95.133.96:9090 stream.sh create 1920x1080 4000k

HELP
        ;;
esac
