#!/bin/sh
apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq python3 procps >/dev/null 2>&1
cp /test/wb /tmp/wb && chmod +x /tmp/wb
rm -rf /tmp/runtime /tmp/workbench-output
/tmp/wb --silent >/tmp/wb.out 2>&1 &
PID=$!
sleep 8
echo "=== SIGQUIT dump ==="
kill -QUIT $PID
sleep 2
echo "=== wb.out (head 160 lines) ==="
head -160 /tmp/wb.out
kill $PID 2>/dev/null || true
