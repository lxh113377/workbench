#!/bin/sh
# 诊断：取几个断言的真实读数，避免把阈值拍脑袋改到能过
set -u
ROOT=/w/root3; BIN=$ROOT/bin; WB=$BIN/workbench-linux
mkdir -p "$BIN/runtime/dsh-home/our-free-model" "$BIN/workbench-output/logs"
cp /w/workbench-linux "$WB"; chmod +x "$WB"
cp /test/fixtures-catalog.json "$BIN/runtime/dsh-home/our-free-model/catalog.json"
cp /test/fixtures-availability.json "$BIN/runtime/dsh-home/our-free-model/availability.json"
cp /test/fixtures-settings.json "$BIN/runtime/dsh-home/our-free-model/settings.json"
cat > "$BIN/workbench-output/config.json" <<'CONF'
{"schemaVersion":3,"autoResume":false,"resumeOnBoot":false,"autoStart":false,
 "server":{"host":"127.0.0.1","port":17902,"openBrowser":false},
 "pool":{"enabled":true,"probeSeconds":3600,"syncSeconds":3600,"strategy":"priority","deepProbe":false}}
CONF
export HOME=/root DSH_OPENAI_GATEWAY_API_KEY=e2e-mock-key-0123456789abcdef DSH_OPENAI_GATEWAY_PORT=8326
printf normal > /tmp/mode
/test/bin/mockofm -addr 127.0.0.1:8326 -key e2e-mock-key-0123456789abcdef -mode normal -modefile /tmp/mode > /tmp/mock.log 2>&1 &
MP=$!
i=0; while [ $i -lt 20 ]; do grep -q MOCKOFM_READY /tmp/mock.log && break; i=$((i+1)); sleep 1; done
"$WB" --silent > /tmp/p.log 2>&1 &
PP=$!
i=0; while [ $i -lt 30 ]; do [ -f "$BIN/runtime/panel.url" ] && break; i=$((i+1)); sleep 1; done
P=http://127.0.0.1:17902
"$PA" "$P" POST /api/pool/sync '{}' > /dev/null 2>&1
"$PA" "$P" GET /api/pool > /tmp/s.out 2>&1
sed -n 's/^BODY=//p' /tmp/s.out > /tmp/s.json
/test/bin/pooljson summary /tmp/s.json > /tmp/sum.txt 2>&1
echo "=== 网关在跑时 ==="
grep -E '^(TOTAL|AVAILABLE|ROUTABLE|GATEWAY_MODELS|SYNC_ADDED|SYNC_UPDATED)=' /tmp/sum.txt
echo "ROUTE_COUNT=$(/test/bin/pooljson routes <($WB --model-pool routes) 2>/dev/null | sed -n 's/^ROUTE_COUNT=//p')"
echo "=== big-pickle / mimo 的 spec 行（看 autoDisabled 是否出现在面板响应） ==="
grep -E '^SPEC\|(big-pickle|mimo-v2.6-flash-free)\|' /tmp/sum.txt
echo "=== CLI status 里同一模型（对照面板 redact 是否丢字段） ==="
$WB --model-pool status 2>/dev/null | tr ',' '\n' | grep -A0 -E '"(id|autoDisabled|enabled)":' | grep -B1 -E '"autoDisabled"' | head -8
printf unauthorized > /tmp/mode
"$PA" "$P" POST /api/pool/probe '{}' > /dev/null 2>&1
"$PA" "$P" GET /api/pool > /tmp/s2.out 2>&1
sed -n 's/^BODY=//p' /tmp/s2.out > /tmp/s2.json
/test/bin/pooljson summary /tmp/s2.json > /tmp/sum2.txt 2>&1
echo "=== 鉴权被拒后 ==="
grep -E '^(PROBE_OK|PROBE_DOWN|PROBE_UNKNOWN|AVAILABLE|GATEWAY_STATUS)=' /tmp/sum2.txt
grep -E '^SPEC\|z-ai/glm-5.3\|' /tmp/sum2.txt
kill $MP $PP 2>/dev/null
echo DIAG2_DONE
