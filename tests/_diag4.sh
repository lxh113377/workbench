#!/bin/sh
apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq python3 procps >/dev/null 2>&1
cp /test/wb /tmp/wb && chmod +x /tmp/wb
rm -rf /tmp/runtime /tmp/workbench-output /root/userproj
mkdir -p /root/userproj
echo "--- prefix CLI (same as e2e) ---"
/tmp/wb --workspace select /root/userproj >/dev/null
/tmp/wb --workspace guard /root/userproj >/dev/null 2>&1 || true
/tmp/wb --selftest-workspace /root/userproj >/dev/null 2>&1 || true
echo "runtime after prefix:"; ls /tmp/runtime
echo "--- start server ---"
/tmp/wb --silent >/tmp/wb.out 2>&1 &
PID=$!
for n in 1 2 3 4 5 6 8 10 15 20; do
  sleep 2
  if [ -f /tmp/runtime/panel.url ]; then echo "panel.url appeared by ~$((n*2))s"; break; fi
done
echo "runtime now:"; ls -la /tmp/runtime
echo "wb.out:"; cat /tmp/wb.out
echo "wb.out size: $(wc -c < /tmp/wb.out)"
if [ -f /tmp/runtime/panel.url ]; then echo "URL=$(cat /tmp/runtime/panel.url)"; fi
kill $PID 2>/dev/null || true
