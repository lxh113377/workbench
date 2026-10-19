#!/bin/sh
set -x
apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq python3 curl procps >/dev/null 2>&1
cp /test/wb /tmp/wb && chmod +x /tmp/wb
rm -rf /tmp/runtime /tmp/workbench-output /root/userproj
mkdir -p /root/userproj
/tmp/wb --workspace select /root/userproj >/dev/null
/tmp/wb --workspace guard /root/userproj >/dev/null 2>&1 || true
/tmp/wb --selftest-workspace /root/userproj >/dev/null 2>&1 || true
echo "=== before server: runtime dir ==="
ls -la /tmp/runtime
echo "=== starting server ==="
/tmp/wb --silent >/tmp/wb.out 2>&1 &
PID=$!
sleep 5
echo "=== ps ==="
ps -ef | grep "[w]b" || echo "no process"
echo "=== runtime after 5s ==="
ls -la /tmp/runtime
echo "=== wb.out ==="; cat /tmp/wb.out
echo "=== logs ==="; ls -la /tmp/workbench-output/logs 2>/dev/null; tail -5 /tmp/workbench-output/logs/workbench.jsonl 2>/dev/null
kill $PID 2>/dev/null || true
