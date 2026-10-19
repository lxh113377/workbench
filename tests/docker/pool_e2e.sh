#!/bin/sh
# tests/docker/pool_e2e.sh — 模型池容器内端到端验收（POSIX sh，兼容 dash 与 busybox ash）
#
# 为什么在容器里跑（老大 09:54 令「用 Docker 虚拟机模拟，别乱折腾了」）：
#   1. 本机反复重建 EXE、与并行会话抢文件，属于"折腾"；容器是一次性干净环境。
#   2. 「别人机器上能不能跑」只能靠干净容器实证，本机绿不算数。
#   3. 容器内 127.0.0.1:8326 是容器自己的回环，宿主 OFM 网关绑回环连不到 ——
#      所以用 mock 网关，顺带让「鉴权被拒 / 上游 5xx / 端口没开」三种形态可控。
#
# 反假绿三条（本轮 mock_selfcheck 实测踩到，全部照此实现）：
#   a. 不用 wget：debian:bookworm-slim 没有它，busybox wget 对 4xx 只给非 0 退出码，
#      与"工具缺失 127"撞值 ⇒ "应当被拒"的断言会假绿。统一用自带 httpget 取真实状态码。
#   b. 工具自检不过就 bail 退出，绝不带着坏链路继续计数。
#   c. 临时文件写 /tmp；/test 以只读挂载（防脚本误写宿主仓）。
#
# 五条目标的断言分别在 1/2/3/4/5 段。判据：末行 E2E_PASS ok=N total=N 且 ok==total。
set -u

ROOT=/root/wb
BIN=$ROOT/bin
WB=$BIN/workbench-linux
DSH=$BIN/runtime/dsh-home
OFM=$DSH/our-free-model
OUT=$BIN/workbench-output
POOL=$OUT/models
MOCK=/test/bin/mockofm
PJ=/test/bin/pooljson
PA=/test/bin/poolapi
HG=/test/bin/httpget
MOCK_KEY=e2e-mock-key-0123456789abcdef
MOCK_MODE=/tmp/mock-mode
MOCK_LOG=/tmp/mock.log
GW=http://127.0.0.1:8326/v1/models

ok=0; fail=0; total=0; FAILED=""; MOCK_PID=""; PANEL_PID=""; PANEL=""

ck() { total=$((total+1)); if [ "$2" = "$3" ]; then ok=$((ok+1)); echo "  [PASS] $1"; else fail=$((fail+1)); FAILED="$FAILED |$1"; echo "  [FAIL] $1 期望=$2 实际=$3"; fi; }
ck_ge() { total=$((total+1)); if [ "${3:-0}" -ge "$2" ] 2>/dev/null; then ok=$((ok+1)); echo "  [PASS] $1 (>=$2 实际=$3)"; else fail=$((fail+1)); FAILED="$FAILED |$1"; echo "  [FAIL] $1 期望>=$2 实际=$3"; fi; }
has() { total=$((total+1)); case "$2" in *"$3"*) ok=$((ok+1)); echo "  [PASS] $1";; *) fail=$((fail+1)); FAILED="$FAILED |$1"; echo "  [FAIL] $1 未含[$3]";; esac; }
nothas() { total=$((total+1)); case "$2" in *"$3"*) fail=$((fail+1)); FAILED="$FAILED |$1"; echo "  [FAIL] $1 不该含[$3]";; *) ok=$((ok+1)); echo "  [PASS] $1";; esac; }
grab() { sed -n "s/^$2=//p" "$1" | head -n 1; }
spec_field() { sed -n "s#^SPEC|$2|.*|$3=\([^|]*\).*#\1#p" "$1" | head -n 1; }
spec_state() { sed -n "s#^SPEC|$2|.*|state=\([^|]*\).*#\1#p" "$1" | head -n 1; }
gt0() { if [ "${1:-0}" -gt 0 ] 2>/dev/null; then echo 1; else echo 0; fi; }
sec_of() { "$HG" "$1" "${2:-}" 2>/dev/null | sed -n "s/^HTTP_STATUS=//p" | head -n 1; }
log() { echo ""; echo "== $* =="; }
bail() { echo ""; echo "  [BAIL] $1"; echo "E2E_BAIL $1"; exit 1; }
cleanup() { [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null; [ -n "$PANEL_PID" ] && kill "$PANEL_PID" 2>/dev/null; return 0; }
trap cleanup EXIT INT TERM

# ── 0. 前置：工具齐备 + 容器环境 + mock 网关 + 面板进程 ───────────
log "0. 前置检查与容器环境"
for f in "$MOCK" "$PJ" "$PA" "$HG"; do [ -x "$f" ] || bail "缺夹具可执行文件 $f"; done
mkdir -p "$BIN" "$OFM" "$DSH/tasks" "$OUT/logs" || bail "无法建容器内目录"
printf normal > "$MOCK_MODE" || bail "无法写 $MOCK_MODE"
# 二进制查找顺序：/w（宿主驱动放现成产物的地方）→ /test → /test/bin
for cand in /w/workbench-linux /test/workbench-linux /test/bin/workbench-linux; do
  if [ -f "$cand" ]; then cp "$cand" "$WB" 2>/dev/null && break; fi
done
[ -x "$WB" ] || bail "找不到可用的 workbench linux 二进制（用 -Binary 指定或先构建）"
chmod +x "$WB"
# 数据面夹具来自 /test/testdata（单一数据源：诊断脚本与 e2e 共用同一份，
# 早先两边各存一份 heredoc，改一处漏一处必然漂移）
for f in catalog.json availability.json settings.json; do
  cp "/test/testdata/$f" "$OFM/$f" || bail "缺 /test/testdata/$f"
done
cp /test/testdata/config.json "$OUT/config.json" || bail "缺 config 夹具"
export DSH_OPENAI_GATEWAY_API_KEY="$MOCK_KEY"
export DSH_OPENAI_GATEWAY_PORT=8326
export HOME=/root
ck "二进制可执行" 0 "$("$WB" --version >/dev/null 2>&1; echo $?)"
echo "  version: $("$WB" --version 2>&1 | head -n 1)"

"$MOCK" -addr 127.0.0.1:8326 -key "$MOCK_KEY" -mode normal -modefile "$MOCK_MODE" > "$MOCK_LOG" 2>&1 &
MOCK_PID=$!
i=0; while [ $i -lt 25 ]; do grep -q MOCKOFM_READY "$MOCK_LOG" 2>/dev/null && break; i=$((i+1)); sleep 1; done
grep -q MOCKOFM_READY "$MOCK_LOG" 2>/dev/null || bail "mock 网关未就绪: $(head -n 3 "$MOCK_LOG" 2>/dev/null | tr "\n" " ")"
# 工具自检：拿不到 200 就说明 mock/httpget 链路坏，后面所有"期望非200"都会假绿
SELF=$(sec_of "$GW" "$MOCK_KEY")
[ "$SELF" = "200" ] || bail "自检失败：normal+正确key 应 200，实际 $SELF（拒绝继续，避免假绿）"
echo "  [SELFTEST-OK] mock 网关 + httpget 链路可信"

"$WB" --silent > /tmp/panel.log 2>&1 &
PANEL_PID=$!
i=0; while [ $i -lt 40 ]; do
  if [ -f "$BIN/runtime/panel.url" ]; then PANEL=$(sed 's#/*$##' "$BIN/runtime/panel.url"); break; fi
  i=$((i+1)); sleep 1
done
ck "面板已启动" 1 "$([ -n "$PANEL" ] && echo 1 || echo 0)"
echo "  panel=$PANEL"
"$PA" "$PANEL" GET /api/pool > /tmp/a0.out 2>&1
ck "面板 /api/pool 可达" HTTP_STATUS=200 "$(head -n 1 /tmp/a0.out)"

# ── 1. 目标① 多接口统一纳管 ──────────────────────────────────────
# 说明：断言一律走面板进程。CLI 每次新建 Service，lastSync/lastProbe 是内存态
# 且未解析网关密钥（实测 SYNC_ADDED=0、探测全 unknown），拿它断言等于测错对象。
log "1. 目标① 从网关 + catalog 发现并纳管"
"$PA" "$PANEL" POST /api/pool/sync '{}' > /tmp/sync.out 2>&1
ck "面板同步 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/sync.out)"
"$PA" "$PANEL" GET /api/pool > /tmp/st.out 2>&1
sed -n 's/^BODY=//p' /tmp/st.out > /tmp/status.json
"$PJ" summary /tmp/status.json > /tmp/sum.txt 2>&1
ck "网关广播 8 个模型" 8 "$(grab /tmp/sum.txt GATEWAY_MODELS)"
ck "同步处理了全部 8 个广播模型" 8 "$(( $(grab /tmp/sum.txt SYNC_ADDED) + $(grab /tmp/sum.txt SYNC_UPDATED) ))"
ck_ge "入池总数>=8" 8 "$(grab /tmp/sum.txt TOTAL)"
ck "存在 free 车道" 1 "$(gt0 "$(grep -c "lane=free" /tmp/sum.txt || true)")"
ck "存在 kilo 车道" 1 "$(gt0 "$(grep -c "lane=kilo" /tmp/sum.txt || true)")"
ck "存在 eac 车道" 1 "$(gt0 "$(grep -c "lane=eac" /tmp/sum.txt || true)")"
ck "网关专属车道不可路由" false "$(spec_field /tmp/sum.txt deepseek-official/deepseek-flash routable)"
ck "地区受限模型默认停用" false "$(spec_field /tmp/sum.txt beam-free enabled)"
ck "免费车道模型可路由" true "$(spec_field /tmp/sum.txt z-ai/glm-5.3 routable)"
"$WB" --model-pool routes > /tmp/routes.json 2>&1
"$PJ" routes /tmp/routes.json > /tmp/routes.txt 2>&1
ck "路由 provider 全为 our-free-model" 0 "$(grep -c "provider=[^o]" /tmp/routes.txt || true)"
ck_ge "路由表条目>=6" 6 "$(grab /tmp/routes.txt ROUTE_COUNT)"

log "1b. 网关不可达时退回 catalog 数据面（autoDisabled 来源）"
kill "$MOCK_PID" 2>/dev/null; wait "$MOCK_PID" 2>/dev/null; MOCK_PID=""
"$PA" "$PANEL" POST /api/pool/sync '{}' > /tmp/sync2.out 2>&1
ck "断网关后同步仍返回 200" HTTP_STATUS=200 "$(head -n 1 /tmp/sync2.out)"
"$WB" --model-pool status > /tmp/status2.json 2>&1
"$PJ" summary /tmp/status2.json > /tmp/sum2.txt 2>&1
ck "退回 catalog 后仍有模型" 1 "$(gt0 "$(grab /tmp/sum2.txt TOTAL)")"
ck "上游判死模型自动停用" false "$(spec_field /tmp/sum2.txt big-pickle enabled)"
ck "自动停用带 autoDisabled 标记" true "$(spec_field /tmp/sum2.txt big-pickle autoDisabled)"
ck "地区受限也自动停用" true "$(spec_field /tmp/sum2.txt mimo-v2.6-flash-free autoDisabled)"
"$MOCK" -addr 127.0.0.1:8326 -key "$MOCK_KEY" -mode normal -modefile "$MOCK_MODE" > "$MOCK_LOG" 2>&1 &
MOCK_PID=$!
i=0; while [ $i -lt 25 ]; do grep -q MOCKOFM_READY "$MOCK_LOG" 2>/dev/null && break; i=$((i+1)); sleep 1; done
[ "$(sec_of "$GW" "$MOCK_KEY")" = "200" ] || bail "mock 重启后自检失败"

# ── 2. 目标③ 实时探活：正常 / 鉴权被拒 / 上游 5xx / 端口没开 ─────
#
# 为什么每态都重启面板（实测踩到的两个坑）：
#   1. CLI 每次新建 Service —— lastProbe / 网关健康是内存态，
#      所以 `--model-pool status` 里 PROBE_OK 恒 0、GATEWAY_STATUS 恒 0，
#      拿它断言等于断一个永远为空的读数（假红）。
#   2. CLI status 输出的是**未脱敏的原始 Spec**，其 state 字段是 json:"-" 不落 JSON，
#      逐模型状态只有面板的 redactSpec 才有。
#   3. 面板常驻会把上一态的冷却留在内存里污染下一态读数 ⇒ 每态换干净面板。
log "2. 目标③ 探活四态"

stop_panel() { [ -n "$PANEL_PID" ] && kill "$PANEL_PID" 2>/dev/null; wait "$PANEL_PID" 2>/dev/null; PANEL_PID=""; }
start_panel() {
  rm -f "$BIN/runtime/panel.url"
  "$WB" --silent > /tmp/panel.log 2>&1 &
  PANEL_PID=$!
  i=0; while [ $i -lt 40 ]; do
    [ -f "$BIN/runtime/panel.url" ] && PANEL=$(sed 's#/*$##' "$BIN/runtime/panel.url") && break
    i=$((i+1)); sleep 1
  done
  [ -n "$PANEL" ] || bail "面板重启失败"
  # 等首轮探测跑完（PROBE_RUNS>=1）再返回，否则读到的是空读数
  j=0; while [ $j -lt 40 ]; do
    "$PA" "$PANEL" GET /api/pool > /tmp/w.out 2>&1
    sed -n 's/^BODY=//p' /tmp/w.out > /tmp/w.json
    "$PJ" summary /tmp/w.json > /tmp/w.txt 2>&1
    [ "$(grab /tmp/w.txt PROBE_RUNS)" -ge 1 ] 2>/dev/null && break
    j=$((j+1)); sleep 1
  done
  [ "$(grab /tmp/w.txt PROBE_RUNS)" -ge 1 ] 2>/dev/null || bail "面板首轮探测未在 40 秒内完成（PROBE_RUNS=0）"
}
reset_health() {
  sed 's/"cooldownTo": *[0-9]*/"cooldownTo": 0/g; s/"consecFail": *[0-9]*/"consecFail": 0/g; s/"state": *"[a-z]*"/"state": "unknown"/g; s/"probeFail": *[0-9]*/"probeFail": 0/g' "$POOL/pool.json" > /tmp/pool.tmp
  cp /tmp/pool.tmp "$POOL/pool.json"
}
case_probe() { # case_probe <名字> —— 面板探测，直接取 /api/pool/probe 的 summary
  "$PA" "$PANEL" POST /api/pool/probe '{}' > /tmp/cp.out 2>&1
  sed -n 's/^BODY=//p' /tmp/cp.out > /tmp/cp.json
  "$PJ" probe /tmp/cp.json > /tmp/cp.txt 2>&1
  grab /tmp/cp.txt PROBE_OK
}
panel_sum() { # panel_sum <输出文件> —— 面板状态（含逐模型 state）
  "$PA" "$PANEL" GET /api/pool > /tmp/ps.out 2>&1
  sed -n 's/^BODY=//p' /tmp/ps.out > "$1"
  "$PJ" summary "$1" > /tmp/sum.txt 2>&1
}

printf normal > "$MOCK_MODE"
stop_panel; reset_health; start_panel
panel_sum /tmp/status.json
ck "正常态：网关判为已授权" true "$(grab /tmp/sum.txt GATEWAY_AUTHORIZED)"
ck_ge "正常态：探测判可用>0" 1 "$(grab /tmp/sum.txt PROBE_OK)"
ck "正常态：无模型判死" 0 "$(grab /tmp/sum.txt PROBE_DOWN)"
ck "正常态 glm-5.3=healthy" healthy "$(spec_state /tmp/sum.txt z-ai/glm-5.3)"

printf unauthorized > "$MOCK_MODE"
stop_panel; reset_health; start_panel
panel_sum /tmp/status.json
ck "鉴权被拒：网关状态码 401 被记录" 401 "$(grab /tmp/sum.txt GATEWAY_STATUS)"
ck "鉴权被拒不判死模型" 0 "$(grab /tmp/sum.txt PROBE_DOWN)"
ck "鉴权被拒 glm-5.3=unknown" unknown "$(spec_state /tmp/sum.txt z-ai/glm-5.3)"
ck "鉴权被拒池子仍可调度" 1 "$(gt0 "$(grab /tmp/sum.txt AVAILABLE)")"

printf down > "$MOCK_MODE"
stop_panel; reset_health; start_panel
panel_sum /tmp/status.json
ck_ge "上游5xx确有模型判死" 1 "$(grab /tmp/sum.txt PROBE_DOWN)"
ck "上游5xx glm-5.3=down" down "$(spec_state /tmp/sum.txt z-ai/glm-5.3)"
"$WB" --model-pool routes > /tmp/r3.json 2>&1; "$PJ" routes /tmp/r3.json > /tmp/r3.txt 2>&1
ck "被判死的模型不进路由表" 0 "$(grep -c "^ROUTE|z-ai/glm-5.3|" /tmp/r3.txt || true)"

log "2b. 半开恢复（冷却到期自动回候选）"
ck "池子账本已落盘" 1 "$([ -s "$POOL/pool.json" ] && echo 1 || echo 0)"
sed 's/"cooldownTo": *[0-9]*/"cooldownTo": 1/g' "$POOL/pool.json" > /tmp/pool.tmp && cp /tmp/pool.tmp "$POOL/pool.json"
"$WB" --model-pool routes > /tmp/r4.json 2>&1; "$PJ" routes /tmp/r4.json > /tmp/r4.txt 2>&1
ck "冷却到期后重回路由表" 1 "$(grep -c "^ROUTE|z-ai/glm-5.3|" /tmp/r4.txt || true)"

log "2c. 网关端口彻底没开（环境未就绪，不是模型故障）"
kill "$MOCK_PID" 2>/dev/null; wait "$MOCK_PID" 2>/dev/null; MOCK_PID=""
stop_panel; reset_health; start_panel
panel_sum /tmp/status.json
ck "端口没开不判死模型" 0 "$(grab /tmp/sum.txt PROBE_DOWN)"
ck "端口没开时模型仍可调度" 1 "$(gt0 "$(grab /tmp/sum.txt AVAILABLE)")"
ck "端口没开 glm-5.3=unknown" unknown "$(spec_state /tmp/sum.txt z-ai/glm-5.3)"
"$MOCK" -addr 127.0.0.1:8326 -key "$MOCK_KEY" -mode normal -modefile "$MOCK_MODE" > "$MOCK_LOG" 2>&1 &
MOCK_PID=$!
i=0; while [ $i -lt 25 ]; do grep -q MOCKOFM_READY "$MOCK_LOG" 2>/dev/null && break; i=$((i+1)); sleep 1; done
printf normal > "$MOCK_MODE"
[ "$(sec_of "$GW" "$MOCK_KEY")" = "200" ] || bail "mock 重启后自检失败"
stop_panel; reset_health; start_panel
# ── 3. 目标② 轮询调度策略 ───────────────────────────────────────
log "3. 目标② 调度策略"
# 段2 的 down 态会把整池打入冷却，且 2c 探测会用内存里的旧 cooldownTo 覆盖
# 2b 手工改过的账本。不先重置，下面的"排序断言"实际测的是"冷却是否清空"。
for id in z-ai/glm-5.3 openai/gpt-oss-20b z-ai/glm-5.3-flash deepseek-ai/deepseek-v4.1-flash; do
  "$WB" --model-pool reset "$id" > /dev/null 2>&1
done
"$WB" --model-pool status > /tmp/status.json 2>&1; "$PJ" summary /tmp/status.json > /tmp/sum.txt 2>&1
ck "重置后 glm-5.3 重回候选" 1 "$(gt0 "$(grab /tmp/sum.txt AVAILABLE)")"
"$WB" --model-pool strategy round-robin > /tmp/st1.out 2>&1
has "CLI 接受合法策略" "$(cat /tmp/st1.out)" "已切换为 round-robin"
"$WB" --model-pool strategy bogus > /tmp/stbad.out 2>&1
has "CLI 拒绝非法策略" "$(cat /tmp/stbad.out)" 未知的调度策略
for st in round-robin weighted least-loaded priority; do
  "$PA" "$PANEL" POST /api/pool/strategy "{\"strategy\":\"$st\"}" > /tmp/sp.out 2>&1
  "$PA" "$PANEL" GET /api/pool > /tmp/sc.out 2>&1
  ck "面板切到 $st 并生效" "\"strategy\":\"$st\"" "$(sed -n "s/^BODY=//p" /tmp/sc.out | grep -o "\"strategy\":\"[a-z-]*\"" | head -n 1)"
done
"$PA" "$PANEL" POST /api/pool/strategy '{"strategy":"bogus"}' > /tmp/sb.out 2>&1
ck "面板拒绝非法策略" HTTP_STATUS=400 "$(head -n 1 /tmp/sb.out)"
"$WB" --model-pool routes > /tmp/rp.json 2>&1; "$PJ" routes /tmp/rp.json > /tmp/rp.txt 2>&1
ck "priority 首位是最高优先级" z-ai/glm-5.3 "$(grep "^ROUTE|" /tmp/rp.txt | head -n 1 | sed "s/^ROUTE|//; s/|.*//")"

# ── 4. 目标⑤ 面板 HTTP API（可配置管理界面） ────────────────────
log "4. 目标⑤ 面板 API 增删改与优先级"
"$PA" "$PANEL" GET /api/pool > /tmp/a1.out 2>&1
B1=$(sed -n "s/^BODY=//p" /tmp/a1.out)
has "响应含 specs" "$B1" "\"specs\""
has "响应含车道水位 lanes" "$B1" "\"lanes\""
has "响应含状态日志 statusLog" "$B1" "\"statusLog\""
nothas "列表不回显密钥明文" "$B1" sk-e2e-secret
POOLAPI_BAD_TOKEN=1 "$PA" "$PANEL" GET /api/pool > /tmp/a2.out 2>&1
ck "无有效 token 被拒（鉴权围栏）" HTTP_STATUS=401 "$(head -n 1 /tmp/a2.out)"
NEW="{\"id\":\"vendor/e2e-model\",\"label\":\"E2E 自定义\",\"model\":\"e2e-model\",\"baseUrl\":\"http://127.0.0.1:8326/v1\",\"apiKey\":\"sk-e2e-secret-7788\",\"format\":\"openai-chat\",\"lane\":\"custom\",\"tier\":\"fast\",\"priority\":1,\"weight\":10,\"enabled\":true,\"routable\":true}"
"$PA" "$PANEL" POST /api/pool/models "$NEW" > /tmp/add.out 2>&1
ck "新增自定义端点 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/add.out)"
AB=$(sed -n "s/^BODY=//p" /tmp/add.out)
has "新增响应含 id" "$AB" "\"id\":\"vendor/e2e-model\""
has "密钥只回显指纹" "$AB" 7788
nothas "新增响应不含密钥明文" "$AB" "sk-e2e-secret-7788"
EDIT="{\"label\":\"E2E 已改\",\"model\":\"e2e-model\",\"baseUrl\":\"http://127.0.0.1:8326/v1\",\"format\":\"openai-chat\",\"lane\":\"custom\",\"tier\":\"quality\",\"priority\":2,\"weight\":20,\"maxFail\":5,\"cooldownSec\":300,\"enabled\":true,\"routable\":true}"
"$PA" "$PANEL" PUT "/api/pool/models/vendor/e2e-model" "$EDIT" > /tmp/edit.out 2>&1
ck "编辑带斜杠 id 模型 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/edit.out)"
"$PA" "$PANEL" GET /api/pool > /tmp/a3.out 2>&1
has "编辑后 label 生效" "$(sed -n "s/^BODY=//p" /tmp/a3.out)" "\"label\":\"E2E 已改\""
"$PA" "$PANEL" POST "/api/pool/models/vendor/e2e-model/disable" '{}' > /tmp/d1.out 2>&1
ck "停用模型 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/d1.out)"
"$PA" "$PANEL" POST "/api/pool/models/vendor/e2e-model/enable" '{}' > /tmp/d2.out 2>&1
ck "启用模型 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/d2.out)"
"$PA" "$PANEL" POST "/api/pool/models/z-ai/glm-5.3/restore" '{}' > /tmp/d3.out 2>&1
ck "恢复冷却（带斜杠 id）HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/d3.out)"
"$PA" "$PANEL" GET /api/pool > /tmp/a4.out 2>&1
sed -n "s/^BODY=//p" /tmp/a4.out > /tmp/b4.json
"$PJ" ids /tmp/b4.json > /tmp/ids.txt 2>&1
LIST=$(grep -v "^vendor/e2e-model$" /tmp/ids.txt | sed -e 's/.*/"&",/' | tr -d '\n' | sed 's/,$//')
"$PA" "$PANEL" POST /api/pool/reorder "{\"ids\":[\"vendor/e2e-model\",$LIST]}" > /tmp/ro.out 2>&1
ck "批量调优先级 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/ro.out)"
"$WB" --model-pool routes > /tmp/r5.json 2>&1; "$PJ" routes /tmp/r5.json > /tmp/r5.txt 2>&1
ck "调优先级后排第一" vendor/e2e-model "$(grep "^ROUTE|" /tmp/r5.txt | head -n 1 | sed "s/^ROUTE|//; s/|.*//")"
"$PA" "$PANEL" POST /api/pool/sync '{}' > /tmp/ps.out 2>&1
ck "面板触发同步 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/ps.out)"
"$PA" "$PANEL" POST /api/pool/probe '{}' > /tmp/pp.out 2>&1
ck "面板触发探测 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/pp.out)"
"$PA" "$PANEL" DELETE "/api/pool/models/vendor/e2e-model" > /tmp/del.out 2>&1
ck "删除模型 HTTP 200" HTTP_STATUS=200 "$(head -n 1 /tmp/del.out)"
has "删除响应回显 id" "$(cat /tmp/del.out)" "\"deleted\":\"vendor/e2e-model\""
"$PA" "$PANEL" GET /api/pool > /tmp/a5.out 2>&1
# 原判据对整段 body grep，会命中 statusLog 里的历史留痕（删除本就该在日志留名），
# 属断言写错而非产品缺陷。改走 pooljson ids 只解析 specs 段取权威读数，
# 并反向确认删除动作确实留了痕（可追溯性要求）。
sed -n "s/^BODY=//p" /tmp/a5.out > /tmp/a5.json
"$PJ" ids /tmp/a5.json > /tmp/ids5.txt 2>&1
ck "删除后 specs 清单已无该模型" 0 "$(grep -c '^vendor/e2e-model$' /tmp/ids5.txt || true)"
ck "删除动作在状态日志留痕（可追溯）" 1 "$(grep -c 'vendor/e2e-model' /tmp/a5.json | awk '{print ($1>0)?1:0}')"
"$HG" "$PANEL/modelpool" -body > /tmp/page.out 2>&1
ck "模型池页面 HTTP 200" 200 "$(sed -n "s/^HTTP_STATUS=//p" /tmp/page.out | head -n 1)"
has "模型池页面含标题" "$(cat /tmp/page.out)" 模型池
has "模型池页面加载脚本" "$(cat /tmp/page.out)" /assets/pool.js

# ── 5. 目标④ 状态日志留痕 ───────────────────────────────────────
log "5. 目标④ 状态日志"
"$PA" "$PANEL" GET "/api/pool/log?tail=400" > /tmp/lg.out 2>&1
LB=$(sed -n "s/^BODY=//p" /tmp/lg.out)
for kind in sync probe recover crud; do
  ck "状态日志含 kind=$kind" 1 "$(grep -c "^LOGKIND|$kind|" /tmp/sum.txt | awk '{print ($1>0)?1:0}')"
done
# skip 是"任务级失败不计模型健康度"的留痕，可能本轮没触发；单独宽松判定
ck_ge "状态日志 kind 种类>=4" 4 "$(grep -c '^LOGKIND|' /tmp/sum.txt || true)"
# switch/skip 需要真实执行失败才会产生；容器内没有 DSH headless，
# 制造不出该事件 —— 记为 UNVERIFIED，不折算通过（本机运行日志已实测到 switch 记录）。
echo "  [UNVERIFIED] kind=switch/skip：容器内无 DSH 执行通道，需本机日志佐证"
ck "model-status.jsonl 已落盘" 1 "$([ "$(wc -l < "$POOL/model-status.jsonl" 2>/dev/null || echo 0)" -gt 0 ] && echo 1 || echo 0)"

# ── 6. 跨进程持久化 ─────────────────────────────────────────────
log "6. 跨进程持久化"
kill "$PANEL_PID" 2>/dev/null; wait "$PANEL_PID" 2>/dev/null; PANEL_PID=""
"$WB" --model-pool status > /tmp/s2.json 2>&1; "$PJ" summary /tmp/s2.json > /tmp/su2.txt 2>&1
ck_ge "重启进程后模型仍在池内" 8 "$(grab /tmp/su2.txt TOTAL)"
"$PJ" ids /tmp/s2.json > /tmp/ids6.txt 2>&1
ck "自定义模型已删净（specs 口径）" 0 "$(grep -c '^vendor/e2e-model$' /tmp/ids6.txt || true)"
has "同步统计跨进程留痕" "$(cat /tmp/su2.txt)" SYNC_UPDATED

log "汇总"
echo "  检查项=$total 通过=$ok 失败=$fail"
if [ "$fail" -gt 0 ]; then echo "  失败项:$FAILED"; echo "E2E_FAIL($fail/$total)"; exit 1; fi
if [ "$ok" -ne "$total" ]; then echo "E2E_FAIL(计数不自洽 ok=$ok total=$total)"; exit 1; fi
if [ "$total" -lt 50 ]; then echo "E2E_FAIL(检查项过少 total=$total，疑空跑)"; exit 1; fi
echo "E2E_PASS ok=$ok total=$total"
exit 0
