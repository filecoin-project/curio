#!/bin/bash
set -e

WORKDIR="$(cd "$(dirname "$0")" && pwd)"
DAEMON="$WORKDIR/target/debug/cuzk-daemon"
BENCH="$WORKDIR/target/debug/cuzk-bench"
PORT=9826
LOGFILE="/tmp/cuzk-e2e-daemon.log"

# Cleanup
pkill -9 -f 'cuzk-daemon' 2>/dev/null || true
sleep 1

echo "=== Starting daemon on port $PORT ==="
FIL_PROOFS_PARAMETER_CACHE=/var/tmp/filecoin-proof-parameters \
  "$DAEMON" --listen "127.0.0.1:$PORT" --log-level info > "$LOGFILE" 2>&1 &
DPID=$!
sleep 3

if ! kill -0 "$DPID" 2>/dev/null; then
    echo "ERROR: daemon failed to start"
    cat "$LOGFILE"
    exit 1
fi
echo "Daemon running (PID=$DPID)"

echo ""
echo "=== GetStatus RPC ==="
"$BENCH" --addr "http://127.0.0.1:$PORT" status 2>&1 || echo "STATUS FAILED"

echo ""
echo "=== Prove PoRep C2 (will fail due to missing 32G params) ==="
timeout 60 "$BENCH" --addr "http://127.0.0.1:$PORT" --log-level warn single -t porep --c1 /data/32gbench/c1.json 2>&1 || echo "(expected failure)"

echo ""
echo "=== GetStatus after proof ==="
"$BENCH" --addr "http://127.0.0.1:$PORT" status 2>&1 || echo "STATUS FAILED"

echo ""
echo "=== GetMetrics ==="
"$BENCH" --addr "http://127.0.0.1:$PORT" metrics 2>&1 || echo "METRICS FAILED"

echo ""
echo "=== Daemon log (last 20 lines) ==="
tail -20 "$LOGFILE" | sed 's/\x1b\[[0-9;]*m//g'

# Cleanup
kill "$DPID" 2>/dev/null || true
wait "$DPID" 2>/dev/null || true
echo ""
echo "=== Done ==="
