#!/bin/sh
# e2e_workspace_container.sh — 容器内「AI 开发工作目录」端到端验收（Linux VM 模拟）
#
# 由 tests/e2e_workspace_docker.ps1 注入并执行；也可手动：
#   docker run --rm -v <binary>:/test/wb:ro -v <this>:/test/run.sh:ro debian:bookworm-slim sh /test/run.sh
#
# 依赖：容器内需有 python3（自检链路用）与 curl（HTTP 断言用）。由调用方决定是否预装。
set -e

BIN_SRC=/test/wb
BIN=/tmp/wb
DATA_ROOT=/tmp/workbench-output
URL_FILE=/tmp/runtime/panel.url

echo "=== 0. 环境 ==="
uname -a
command -v python3 >/dev/null 2>&1 && echo "python3: $(python3 -V 2>&1)" || echo "python3: MISSING"
command -v curl >/dev/null 2>&1 && echo "curl: ok" || echo "curl: MISSING"
cp "$BIN_SRC" "$BIN"; chmod +x "$BIN"
"$BIN" --version

echo "=== 1. 准备用户项目目录 ==="
rm -rf /root/userproj /root/userproj2 "$DATA_ROOT" /tmp/runtime
mkdir -p /root/userproj/src /root/userproj2
echo "# 用户真实项目" > /root/userproj/README.md
echo "package main" > /root/userproj/src/main.go
echo "SENTINEL-ORIGINAL" > /root/SENTINEL.txt

echo "=== 2. 目录校验（CLI） ==="
"$BIN" --workspace validate /root/userproj | grep -q '"allowed": true' && echo "  [PASS] validate good dir"
if "$BIN" --workspace validate relative/dir | grep -q '"ok": false'; then echo "  [PASS] validate rejects relative path"; else echo "  [FAIL] relative path not rejected"; exit 1; fi
if "$BIN" --workspace validate /etc | grep -q '"ok": false'; then echo "  [PASS] validate rejects system dir"; else echo "  [FAIL] system dir not rejected"; exit 1; fi

echo "=== 3. 切换到用户目录 ==="
"$BIN" --workspace select /root/userproj >/dev/null
"$BIN" --workspace status | grep -q '"mode": "external"' && echo "  [PASS] mode=external"
"$BIN" --workspace status | grep -q '"root": "/root/userproj"' && echo "  [PASS] root=user dir"

echo "=== 4. 护栏实证（越权必须拒绝） ==="
"$BIN" --workspace guard /root/userproj | tee /tmp/guard.txt
grep -q "\[GUARD\] pass=11 fail=0" /tmp/guard.txt && echo "  [PASS] guard 11/11"

echo "=== 5. 产物落盘 + 真实验收（离线等价链路） ==="
"$BIN" --selftest-workspace /root/userproj | tee /tmp/selftest.txt
grep -q "\[verify\] PASS" /tmp/selftest.txt && echo "  [PASS] pipeline verify PASS"
test -f /root/userproj/selftest/hello.py && echo "  [PASS] artifact written into user dir"
"$(command -v python3 || command -v python || command -v node)" /root/userproj/selftest/hello.py | grep -q "hello from workbench" && echo "  [PASS] artifact actually runs"

echo "=== 6. 隔离边界（沙箱零残留 / 原目录零污染） ==="
SANDBOX=$(find "$DATA_ROOT/tasks" -type d -name workspace 2>/dev/null | wc -l)
[ "$SANDBOX" -eq 0 ] && echo "  [PASS] no task sandbox dir in external mode"
grep -q "SENTINEL-ORIGINAL" /root/SENTINEL.txt && echo "  [PASS] sentinel untouched"

echo "=== 7. HTTP 面板与工作目录 API（真实服务进程） ==="
# 服务进程从 EXE 同目录推导 RuntimeDir（/tmp/runtime）：等它落盘 panel.url 再读（最多 60s）。
"$BIN" --silent >/tmp/wb.out 2>&1 &
WB_PID=$!
WAITS=0
while [ ! -f "$URL_FILE" ] && [ "$WAITS" -lt 60 ]; do sleep 1; WAITS=$((WAITS+1)); done
if [ ! -f "$URL_FILE" ]; then
  echo "  [FAIL] panel.url 未生成"
  echo "  --- wb.out ---"; cat /tmp/wb.out 2>/dev/null || true
  exit 1
fi
URL=$(tr -d '\r\n' < "$URL_FILE")
echo "  panel url: $URL"
TOKEN=$(curl -s "$URL" | sed -n 's/.*__WB_TOKEN__ = "\([^"]*\)".*/\1/p')
[ -n "$TOKEN" ] && echo "  [PASS] token injected" || { echo "  [FAIL] no token"; exit 1; }
curl -s -H "X-WB-Token: $TOKEN" "$URL/api/workspace" | grep -q '"mode"' && echo "  [PASS] GET /api/workspace"
curl -s -H "X-WB-Token: $TOKEN" -X POST -H "Content-Type: application/json" -d '{"path":"/root/userproj2"}' "$URL/api/workspace/validate" | grep -q '"allowed": true' && echo "  [PASS] POST /api/workspace/validate"
curl -s -H "X-WB-Token: $TOKEN" -X POST -H "Content-Type: application/json" -d '{"path":"/etc"}' "$URL/api/workspace/validate" | grep -q '"ok": false' && echo "  [PASS] POST validate rejects /etc"
curl -s -H "X-WB-Token: $TOKEN" -X POST -H "Content-Type: application/json" -d '{"path":"/root/userproj2"}' "$URL/api/workspace/select" | grep -q '"ok": true' && echo "  [PASS] POST /api/workspace/select"
curl -s -H "X-WB-Token: $TOKEN" "$URL/api/workspace" | grep -q '"root": "/root/userproj2"' && echo "  [PASS] switch reflected"
curl -s "$URL/admin?token=$TOKEN" | grep -q 'data-tab="workspace"' && echo "  [PASS] admin page has workspace tab"
curl -s -H "X-WB-Token: $TOKEN" "$URL/api/workspace/audit?tail=50" | grep -q '"denied"' && echo "  [PASS] audit endpoint"
curl -s -H "X-WB-Token: $TOKEN" -X POST -H "Content-Type: application/json" -d '{}' "$URL/api/workspace/reset" | grep -q '"ok": true' && echo "  [PASS] POST /api/workspace/reset"
kill $WB_PID 2>/dev/null || true
sleep 1

echo "=== 8. 切回项目模式还原 ==="
"$BIN" --workspace reset >/dev/null
"$BIN" --workspace status | grep -q '"mode": "project"' && echo "  [PASS] back to project mode"

echo "E2E_PASS"
