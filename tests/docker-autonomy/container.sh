#!/usr/bin/env bash
# container.sh — 容器内自主闭环端到端验收（Docker VM 模拟，确定性通道）
# 由 docker-run.sh / CI 调用；也可手跑：
#   docker run --rm -v "<binary>:/test/wb:ro" -v "<fakedsh>:/test/fakedsh:ro" -v "<this dir>:/dk" debian:bookworm-slim bash -c "apt-get install -y -qq python3 && RUN_ID=r1 bash /dk/container.sh"
set -uo pipefail

BIN=/tmp/wb; FAKE=/tmp/fakedsh; DATA=/tmp/workbench-output; RT=/tmp/runtime
RUN_ID=${RUN_ID:-run-host}
LOG=/dk/evidence/$RUN_ID/run.log
# 不依赖 bash 进程替换/tee（debian:slim 里可能都没有）：直接 exec 重定向到文件，
# 调用方用 `docker logs` 或事后读 evidence/<RUN_ID>/run.log 取证。
mkdir -p /dk/evidence/$RUN_ID
: > "$LOG"
exec > "$LOG" 2>&1

echo "=== 0. 环境 ==="
uname -s -m
command -v python3 >/dev/null 2>&1 && echo "python3: $(python3 -V)" || { echo "python3 MISSING"; exit 3; }
cp /test/wb "$BIN"; chmod +x "$BIN"
cp /test/fakedsh "$FAKE"; chmod +x "$FAKE"
mkdir -p /tmp/resources/app/lib; : > /tmp/resources/app/lib/desktop-cli.js
export DSH_DESKTOP_EXE="$FAKE"
echo "binary: $("$BIN" --version)"

rm -rf "$DATA" "$RT"; mkdir -p "$DATA" "$RT"
cp /dk/config.e2e.json "$DATA/config.json"

echo "=== 1. 离线自检 ==="
"$BIN" --autonomy selftest > /tmp/selftest.txt 2>&1 || { echo SELFTEST_FAIL; cat /tmp/selftest.txt; exit 1; }
grep -q "RESULT: 11 PASS / 0 FAIL" /tmp/selftest.txt && echo "  [PASS] 离线 selftest 11/11"

echo "=== 2. 启动面板服务 ==="
"$BIN" --silent > /tmp/wb.out 2>&1 &
WB_PID=$!
for n in $(seq 1 30); do [ -f "$RT/panel.url" ] && break; sleep 1; done
[ -f "$RT/panel.url" ] || { echo PANEL_URL_MISSING; cat /tmp/wb.out; exit 1; }
python3 /dk/start_engine.py || { echo START_FAIL; exit 1; }

echo "=== 3. 等闭环推进（最多 150 秒） ==="
for n in $(seq 1 30); do
  python3 /dk/poll.py > /tmp/poll.txt 2>&1 || true
  grep -E "^(kinds|goals|raw_)" /tmp/poll.txt | tail -2
  GD=$(grep -o "raw_goalDone=[0-9]*" /tmp/poll.txt | head -1 | cut -d= -f2)
  NG=$(grep -o "raw_nextGoal=[0-9]*" /tmp/poll.txt | head -1 | cut -d= -f2)
  if [ -n "$GD" ] && [ "$GD" -gt 0 ] && [ -n "$NG" ] && [ "$NG" -gt 0 ]; then echo "endpoints reached at iter $n"; break; fi
  sleep 5
done

echo "=== 4. 判据核对 ==="
python3 /dk/judge.py; JUDGE_RC=$?
cp /tmp/workbench-output/autonomy/goals.json /dk/evidence/$RUN_ID/goals.json 2>/dev/null || true
cp /tmp/workbench-output/autonomy/decisions.jsonl /dk/evidence/$RUN_ID/decisions.jsonl 2>/dev/null || true
rm -rf /dk/evidence/$RUN_ID/tasks 2>/dev/null || true
cp -r /tmp/workbench-output/tasks /dk/evidence/$RUN_ID/tasks 2>/dev/null || true
kill $WB_PID 2>/dev/null || true
sleep 1
if [ "$JUDGE_RC" -ne 0 ]; then echo E2E_FAIL; exit 1; fi
echo E2E_PASS
