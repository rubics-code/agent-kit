#!/usr/bin/env bash
# Runs the same turns with toolsearch off and on, and writes the token log.
# Needs OPENROUTER_API_KEY in the environment. Usage: scripts/tokens.sh OUTDIR
set -euo pipefail
out=${1:?usage: tokens.sh OUTDIR}
mkdir -p "$out"
here=$(cd "$(dirname "$0")/.." && pwd)
go build -o "$out/spike" "$here"
B=http://127.0.0.1:8090/api

turn() { # session text
  curl -sf -X POST "$B/apps/spike/users/u/sessions/$1" -H 'Content-Type: application/json' -d '{}' >/dev/null || true
  curl -sf -X POST "$B/run" -H 'Content-Type: application/json' \
    -d "{\"appName\":\"spike\",\"userId\":\"u\",\"sessionId\":\"$1\",\"newMessage\":{\"role\":\"user\",\"parts\":[{\"text\":\"$2\"}]}}" >"$out/$1.json"
}

for mode in off on; do
  flag=false; [ "$mode" = on ] && flag=true
  rm -f "$out/$mode.db"
  "$out/spike" -addr 127.0.0.1:8090 -db "$out/$mode.db" -toolsearch=$flag -tag "$mode" -usage-log "$out/usage.jsonl" 2>"$out/$mode.log" &
  pid=$!
  for _ in $(seq 20); do curl -sf "$B/health" >/dev/null && break; sleep 0.5; done
  turn "$mode-a" "What notes do I have?"
  turn "$mode-b" "What notes do I have?"
  turn "$mode-c" "What notes do I have?"
  turn "$mode-d" "Is the domain rubics-spike-test.com available at the registrar?"
  kill $pid; wait $pid 2>/dev/null || true
done
