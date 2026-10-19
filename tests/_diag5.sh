#!/bin/sh
apt-get update -qq >/dev/null 2>&1; apt-get install -y -qq python3 procps >/dev/null 2>&1
cp /test/wb /tmp/wb && chmod +x /tmp/wb

try_variant() {
  NAME="$1"; shift
  rm -rf /tmp/runtime /tmp/workbench-output /root/userproj
  mkdir -p /root/userproj
  echo "# user" > /root/userproj/README.md
  "$@" >/dev/null 2>&1 || true
  /tmp/wb --silent >/tmp/wb.out 2>&1 &
  PID=$!
  OK=no
  for k in 1 2 3 4 5 6 7 8 9 10 12 15; do
    sleep 1
    if [ -f /tmp/runtime/panel.url ]; then OK=yes; break; fi
  done
  echo "VARIANT [$NAME] panel.url=$OK  wb.out_bytes=$(wc -c < /tmp/wb.out)"
  if [ "$OK" = "no" ]; then
    kill -QUIT $PID 2>/dev/null
    sleep 1
    echo "  --- stacks mentioning workbench (first 40 lines) ---"
    grep -n "workbench/" /tmp/wb.out | head -40
  fi
  kill $PID 2>/dev/null || true
  sleep 1
}

try_variant "no-prefix"      true
try_variant "select-only"    /tmp/wb --workspace select /root/userproj
try_variant "guard-only"     /tmp/wb --workspace guard /root/userproj
try_variant "selftest-only"  /tmp/wb --selftest-workspace /root/userproj
