#!/bin/sh
# 诊断：CLI 独立进程探测在各网关模式下的真实读数（不猜阈值）
set -u
ROOT=/w/root4; BIN=$ROOT/bin; WB=$BIN/workbench-linux
mkdir -p "$BIN/runtime/dsh-home/our-free-model" "$BIN/workbench-output/logs"
cp /w/workbench-linux "$WB"; chmod +x "$WB"
cp /test/testdata/catalog.json "$BIN/runtime/dsh-home/our-free-model/catalog.json"
cp /test/testdata/availability.json "$BIN/runtime/dsh-home/our-free-model/availability.json"
cp /test/testdata/settings.json "$BIN/runtime/dsh-home/our-free-model/settings.json"
export HOME=/root DSH_OPENAI_GATEWAY_API_KEY=e2e-mock-key-0123456789abcdef DSH_OPENAI_GATEWAY_PORT=8326
printf normal > /tmp/mode
/test/bin/mockofm -addr 127.0.0.1:8326 -key e2e-mock-key-0123456789abcdef -mode normal -modefile /tmp/mode > /tmp/mock.log 2>&1 &
MP=$!
i=0; while [ $i -lt 20 ]; do grep -q MOCKOFM_READY /tmp/mock.log && break; i=$((i+1)); sleep 1; done
echo "mock up: $(grep -c MOCKOFM_READY /tmp/mock.log)"
echo "=== sync (CLI) ==="
"$WB" --model-pool sync 2>&1 | head -3
echo "=== probe normal ==="
"$WB" --model-pool probe 2>&1 | head -2
"$WB" --model-pool status > /tmp/s.json 2>&1
/test/bin/pooljson summary /tmp/s.json > /tmp/sum.txt 2>&1
grep -E '^(TOTAL|AVAILABLE|ROUTABLE|PROBE_TOTAL|PROBE_OK|PROBE_DOWN|PROBE_UNKNOWN|GATEWAY_STATUS|GATEWAY_AUTHORIZED|GATEWAY_REACHABLE)=' /tmp/sum.txt
echo "--- glm-5.3 spec 行原文 ---"
grep -E '^SPEC\|z-ai/glm-5.3\|' /tmp/sum.txt
echo "--- mock 侧计数 ---"
/test/bin/httpget http://127.0.0.1:8326/ctl/state -body | sed -n 's/^BODY=//p'
echo "=== 切 unauthorized 再 probe ==="
printf unauthorized > /tmp/mode
"$WB" --model-pool probe 2>&1 | head -2
"$WB" --model-pool status > /tmp/s2.json 2>&1
/test/bin/pooljson summary /tmp/s2.json > /tmp/sum2.txt 2>&1
grep -E '^(PROBE_OK|PROBE_DOWN|PROBE_UNKNOWN|AVAILABLE|GATEWAY_STATUS)=' /tmp/sum2.txt
grep -E '^SPEC\|z-ai/glm-5.3\|' /tmp/sum2.txt
/test/bin/httpget http://127.0.0.1:8326/ctl/state -body | sed -n 's/^BODY=//p'
kill $MP 2>/dev/null
echo DIAG3_DONE
