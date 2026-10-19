#!/bin/sh
# tests/docker/mock_selfcheck.sh — 校验 mock OFM 网关本身（e2e 的地基）
#
# 为什么单独验：pool_e2e.sh 的「鉴权被拒 / 上游 5xx / 端口没开」三态全靠
# mock 可控切换。mock 自己不可信，上面所有断言都是空转。
#
# 反假绿设计（本轮实测踩到）：
#   1. 不用 wget —— debian:bookworm-slim 没有它，busybox wget 对 4xx 只给
#      非 0 退出码，与"工具缺失(127)"撞值，导致"应当被拒"的断言假绿。
#      改用自带 httpget 拿真实 HTTP_STATUS（连不上=000，与 401/502 可区分）。
#   2. 先做工具自检：httpget 拿不到 200 就整轮判红退出，绝不继续计数。
#   3. 临时文件写 /tmp（/test 是只读挂载，实测写不进去）。
set -u
T=/test/bin
M=$T/mockofm
HG=$T/httpget
KEY=e2e-key-123
MODE=/tmp/mock-mode
LOG=/tmp/mock.log
URL=http://127.0.0.1:8326/v1/models

ok=0; fail=0; total=0; FAILED=""
ck() { total=$((total+1)); if [ "$2" = "$3" ]; then ok=$((ok+1)); echo "  [PASS] $1"; else fail=$((fail+1)); FAILED="$FAILED |$1"; echo "  [FAIL] $1 期望=$2 实际=$3"; fi; }
status_of() { "$HG" "$2" "$3" 2>/dev/null | sed -n 's/^HTTP_STATUS=//p' | head -n 1; }
bail() { echo "  [BAIL] $1"; echo "MOCK_CHECK_BAIL"; exit 1; }

[ -x "$M" ] || bail "mockofm 不存在或不可执行: $M"
[ -x "$HG" ] || bail "httpget 不存在或不可执行: $HG"

printf normal > "$MODE" 2>/dev/null || bail "无法写 $MODE"

"$M" -addr 127.0.0.1:8326 -key "$KEY" -mode normal -modefile "$MODE" > "$LOG" 2>&1 &
PID=$!
i=0; while [ $i -lt 20 ]; do grep -q MOCKOFM_READY "$LOG" 2>/dev/null && break; i=$((i+1)); sleep 1; done
grep -q MOCKOFM_READY "$LOG" 2>/dev/null || bail "mock 未就绪: $(cat "$LOG" 2>/dev/null | head -n 3)"

# 工具自检：正常态必须能拿到 200，否则后面的"期望非 200"全是假绿
S=$(status_of 0 "$URL" "$KEY")
if [ "$S" != "200" ]; then
  echo "  [FAIL] 工具自检：normal+正确key 应 200，实际 '$S'"
  kill "$PID" 2>/dev/null
  bail "httpget/mock 链路不通，拒绝继续（避免假绿）"
fi
echo "  [SELFTEST-OK] httpget 能拿到真实 200，后续拒绝类断言可信"

ck "normal + 正确 key => 200" 200 "$S"
ck "normal + 错误 key => 401" 401 "$(status_of 0 "$URL" wrong-key)"
ck "normal + 无 key => 401" 401 "$(status_of 0 "$URL" '')"

printf unauthorized > "$MODE"
ck "unauthorized + 正确 key => 401（modefile 热切生效）" 401 "$(status_of 0 "$URL" "$KEY")"

printf down > "$MODE"
ck "down => 502（上游故障可区分于鉴权失败）" 502 "$(status_of 0 "$URL" "$KEY")"

printf normal > "$MODE"
ck "切回 normal => 恢复 200" 200 "$(status_of 0 "$URL" "$KEY")"

# 清单内容：三类前缀都要在（工作台的分类断言依赖这个形状）
"$HG" "$URL" "$KEY" -body > /tmp/models.out 2>&1
ck "清单可取回" 0 "$?"
ck "our-free-model 前缀=6（含 kilo 车道）" 6 "$(grep -o '"id":"our-free-model/[^"]*"' /tmp/models.out | grep -vc 'region' | tr -d ' ')"
ck "region 前缀=1" 1 "$(grep -o '"id":"our-free-model-region/[^"]*"' /tmp/models.out | wc -l | tr -d ' ')"
ck "网关专属前缀=1" 1 "$(grep -o '"id":"deepseek-official/[^"]*"' /tmp/models.out | wc -l | tr -d ' ')"
ck "清单总数=8" 8 "$(grep -o '"id":"[^"]*"' /tmp/models.out | wc -l | tr -d ' ')"

# 补全端点（deepProbe 路径）
CS=$( "$HG" http://127.0.0.1:8326/v1/chat/completions "$KEY" -post '{"model":"x","messages":[{"role":"user","content":"ping"}]}' | sed -n 's/^HTTP_STATUS=//p' | head -n 1 )
ck "chat/completions => 200" 200 "$CS"

# 计数端点（证明探测真的打了请求，不是空跑）
"$HG" http://127.0.0.1:8326/ctl/state '' -body > /tmp/state.out 2>&1
ck "计数端点 modelsHits>0" 1 "$(grep -o '"modelsHits":[1-9][0-9]*' /tmp/state.out | wc -l | awk '{print ($1>0)?1:0}')"
ck "计数端点 chatHits>0" 1 "$(grep -o '"chatHits":[1-9][0-9]*' /tmp/state.out | wc -l | awk '{print ($1>0)?1:0}')"

# 端口彻底没开（e2e 的 2c 段复用同一判据）
kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null
ck "kill 后端口不可达（000，可与 401 区分）" 000 "$(status_of 0 "$URL" "$KEY")"

echo "  检查项=$total 通过=$ok 失败=$fail"
if [ "$fail" -gt 0 ]; then echo "  失败项:$FAILED"; echo "MOCK_CHECK_FAIL($fail/$total)"; exit 1; fi
if [ "$ok" -ne "$total" ]; then echo "MOCK_CHECK_FAIL(计数不自洽 ok=$ok total=$total)"; exit 1; fi
if [ "$total" -lt 12 ]; then echo "MOCK_CHECK_FAIL(检查项过少 total=$total，疑空跑)"; exit 1; fi
echo "MOCK_CHECK_PASS ok=$ok total=$total"
