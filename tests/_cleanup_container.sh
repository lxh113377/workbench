#!/bin/sh
# 容器内清理：找占 /tmp/wb 或 fakedsh 的进程并杀掉（容器无 ps/pkill）
for p in /proc/[0-9]*; do
  if grep -qa 'tmp/wb' "$p/cmdline" 2>/dev/null; then
    pid=$(basename "$p")
    echo "kill wb pid=$pid"
    kill -9 "$pid" 2>/dev/null || true
  fi
  if grep -qa 'fakedsh' "$p/cmdline" 2>/dev/null; then
    pid=$(basename "$p")
    echo "kill fakedsh pid=$pid"
    kill -9 "$pid" 2>/dev/null || true
  fi
done
sleep 1
rm -rf /tmp/wb /tmp/fakedsh /tmp/workbench-output /tmp/runtime /tmp/token
ls -la /tmp | head -20
echo CLEAN_DONE