#!/bin/sh
set -e
apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq python3 curl >/dev/null 2>&1
cp /test/wb /tmp/wb && chmod +x /tmp/wb
rm -rf /tmp/runtime /tmp/workbench-output
/tmp/wb --silent > /tmp/wb.out 2>&1 &
PID=$!
for n in $(seq 1 60); do
  if [ -f /tmp/runtime/panel.url ]; then echo "url file after Ns"; break; fi
  sleep 1
done
ls -la /tmp/runtime 2>/dev/null || echo "no runtime dir"
echo "--- wb.out ---"; cat /tmp/wb.out
echo "--- procs ---"; ps -ef | grep "[w]b" | head -3
kill $PID 2>/dev/null || true
