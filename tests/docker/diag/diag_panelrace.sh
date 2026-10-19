#!/bin/sh
# 诊断：面板启动后 lastProbe 何时才有值；以及 down 态是否进路由表
set -u
ROOT=/w/root5; BIN=$ROOT/bin; WB=$BIN/workbench-linux
DSH=$BIN/runtime/dsh-home; OFM=$DSH/our-free-model; OUT=$BIN/workbench-output
mkdir -p "$OFM" "$OUT/logs"
cp /w/workbench-linux "$WB"; chmod +x "$WB"
cp /test/testdata/catalog.json "$OFM/catalog.json"
cp /test/testdata/availability.json "$OFM/availability.json"
cp /test/testdata/settings.json "$OFM/settings.json"
cat > "$OUT/config.json" <<'CONF'
{"schemaVersion":3,"autoResume":false,"resumeOnBoot":false,"autoStart":false,
 "server":{"host":"127.0.0.1","port":17903,"openBrowser":false},
 "pool":{"enabled":true,"probeSeconds":3600,"syncSeconds":3600,"strategy":"priority","deepProbe":false}}
CONF
export HOME=/root DSH_OPENAI_GATEWAY_API_KEY=e2e-mock-key-0123456789abcdef DSH_OPENAI_GATEWAY_PORT=8326
printf normal > /tmp/mode
/test/bin/mockofm -addr 127.0.0.1:8326 -key e2e-mock-key-0123456789abcdef -mode normal -modefile /tmp/mode > /tmp/mock.log 2>&1 &
MP=$!
i=0; while [ $i -lt 20 ]; do grep -q MOCKOFM_READY /tmp/mock.log && break; i=$((i+1)); sleep 1; done
"$WB" --silent > /tmp/p.log 2>&1 &
PP=$!
i=0; while [ $i -lt 40 ]; do [ -f "$BIN/runtime/panel.url" ] && break; i=$((i+1)); sleep 1; done
P=http://127.0.0.1:17903
echo "panel.url 出现时立即读:"
"$PA" "$P" GET /api/pool > /tmp/x.out 2>&1
sed -n 's/^BODY=//p' /tmp/x.out > /tmp/x.json
/test/bin/pooljson summary /tmp/x.json > /tmp/x.txt 2>&1
grep -E '^(PROBE_TOTAL|PROBE_OK|PROBE_RUNS|AVAILABLE|GATEWAY_AUTHORIZED)=' /tmp/x.txt
echo "  PROBE_RUNS 行: $(grep PROBE_RUNS /tmp/x.txt)"
echo "等 6 秒后再读:"
sleep 6
"$PA" "$P" GET /api/pool > /tmp/y.out 2>&1
sed -n 's/^BODY=//p' /tmp/y.out > /tmp/y.json
/test/bin/pooljson summary /tmp/y.json > /tmp/y.txt 2>&1
grep -E '^(PROBE_TOTAL|PROBE_OK|PROBE_RUNS|AVAILABLE|GATEWAY_AUTHORIZED)=' /tmp/y.txt
echo "显式 POST /api/pool/probe 后:"
"$PA" "$P" POST /api/pool/probe '{}' > /tmp/z.out 2>&1
sed -n 's/^BODY=//p' /tmp/z.out | head -c 200; echo
echo "down 态路由表判定:"
printf down > /tmp/mode
"$PA" "$P" POST /api/pool/probe '{}' > /dev/null 2>&1
sleep 1
"$WB" --model-pool routes > /tmp/r.json 2>&1
/test/bin/pooljson routes /tmp/r.json > /tmp/r.txt 2>&1
echo "  ROUTE_COUNT=$(sed -n 's/^ROUTE_COUNT=//p' /tmp/r.txt)"
echo "  含 glm-5.3? $(grep -c '^ROUTE|z-ai/glm-5.3|' /tmp/r.txt)"
echo "  面板侧 glm-5.3 state:"
"$PA" "$P" GET /api/pool > /tmp/q.out 2>&1
sed -n 's/^BODY=//p' /tmp/q.out > /tmp/q.json
/test/bin/pooljson summary /tmp/q.json | grep -E '^SPEC\|z-ai/glm-5.3\|'
kill $MP $PP 2>/dev/null
echo DIAG4_DONE
