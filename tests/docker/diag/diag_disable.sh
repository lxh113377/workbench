#!/bin/sh
# 诊断：面板 disable/enable 返回 400 的真实原因（不猜，取响应体）
set -u
ROOT=/w/root2; BIN=$ROOT/bin; WB=$BIN/workbench-linux
mkdir -p "$BIN" "$BIN/workbench-output/logs"
cp /w/workbench-linux "$WB"; chmod +x "$WB"
cat > "$BIN/workbench-output/config.json" <<'CONF'
{"schemaVersion":3,"autoResume":false,"resumeOnBoot":false,"autoStart":false,
 "server":{"host":"127.0.0.1","port":17901,"openBrowser":false},
 "pool":{"enabled":true,"probeSeconds":3600,"syncSeconds":3600,"strategy":"priority","deepProbe":false}}
CONF
export HOME=/root
"$WB" --silent > /tmp/p.log 2>&1 &
PID=$!
i=0; while [ $i -lt 30 ]; do [ -f "$BIN/runtime/panel.url" ] && break; i=$((i+1)); sleep 1; done
P=http://127.0.0.1:17901
echo "panel=$P"
NEW='{"id":"vendor/e2e-model","label":"L","model":"m","baseUrl":"http://127.0.0.1:1/v1","apiKey":"sk-x-1234","format":"openai-chat","lane":"custom","tier":"fast","priority":5,"weight":10,"enabled":true,"routable":true}'
echo "--- add ---"
/test/bin/poolapi "$P" POST /api/pool/models "$NEW"
echo "--- disable (slash id) ---"
/test/bin/poolapi "$P" POST "/api/pool/models/vendor/e2e-model/disable" '{}'
echo "--- disable (escaped id) ---"
/test/bin/poolapi "$P" POST "/api/pool/models/vendor%2Fe2e-model/disable" '{}'
echo "--- restore on plain id z-ai/glm-5.3 ---"
/test/bin/poolapi "$P" POST "/api/pool/models/z-ai/glm-5.3/restore" '{}'
echo "--- delete ---"
/test/bin/poolapi "$P" DELETE "/api/pool/models/vendor/e2e-model"
kill $PID 2>/dev/null
echo DIAG_DONE
