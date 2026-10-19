#!/bin/sh
# 诊断：删除后"清单已无该模型"为何仍命中 —— 是真的没删掉，还是我的 grep 命中了状态日志？
set -u
BIN=/w/root7; WB=$BIN/bin/workbench-linux
mkdir -p "$BIN/bin/runtime/dsh-home/our-free-model" "$BIN/bin/workbench-output/logs"
cp /w/workbench-linux "$WB"; chmod +x "$WB"
OFM=$BIN/bin/runtime/dsh-home/our-free-model
cp /test/fixtures-catalog.json "$OFM/"
cp /test/fixtures-availability.json "$OFM/"
cp /test/fixtures-settings.json "$OFM/"
cat > "$BIN/bin/workbench-output/config.json" <<'CONF'
{"schemaVersion":3,"autoResume":false,"resumeOnBoot":false,"autoStart":false,
 "server":{"host":"127.0.0.1","port":17905,"openBrowser":false},
 "pool":{"enabled":true,"probeSeconds":3600,"syncSeconds":3600,"strategy":"priority","deepProbe":false}}
CONF
export HOME=/root
"$WB" --silent > /tmp/p.log 2>&1 &
PP=$!
i=0; while [ $i -lt 40 ]; do [ -f "$BIN/bin/runtime/panel.url" ] && break; i=$((i+1)); sleep 1; done
P=http://127.0.0.1:17905
sleep 5
NEW='{"id":"vendor/e2e-model","label":"L","model":"m","baseUrl":"http://127.0.0.1:1/v1","apiKey":"sk-x","format":"openai-chat","lane":"custom","tier":"fast","priority":5,"weight":10,"enabled":true,"routable":true}'
/test/bin/poolapi "$P" POST /api/pool/models "$NEW" > /tmp/add.out 2>&1
echo "add: $(sed -n 's/^HTTP_STATUS=//p' /tmp/add.out)"
/test/bin/poolapi "$P" DELETE "/api/pool/models/vendor/e2e-model" > /tmp/d.out 2>&1
echo "del: $(head -c 200 /tmp/d.out | tr '\n' ' ')"
/test/bin/poolapi "$P" GET /api/pool > /tmp/g.out 2>&1
sed -n 's/^BODY=//p' /tmp/g.out > /tmp/g.json
echo "整段 body 命中 vendor/e2e-model 的行数: $(grep -c 'vendor/e2e-model' /tmp/g.json)"
/test/bin/pooljson ids /tmp/g.json > /tmp/ids.txt 2>&1
echo "specs 段命中数（权威判据）: $(grep -c '^vendor/e2e-model$' /tmp/ids.txt)"
echo "--- 命中出现在哪个字段 ---"
tr ',' '\n' < /tmp/g.json | grep 'vendor/e2e-model' | head -4
kill "$PP" 2>/dev/null
echo DELDIAG_DONE
