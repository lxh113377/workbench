#!/bin/sh
# 临时诊断：看 /ctl/state 到底返回什么
printf normal > /tmp/mode
/test/bin/mockofm -addr 127.0.0.1:8326 -key K -mode normal -modefile /tmp/mode > /tmp/l 2>&1 &
PID=$!
i=0; while [ $i -lt 15 ]; do grep -q MOCKOFM_READY /tmp/l && break; i=$((i+1)); sleep 1; done
echo "ready=$(grep -c MOCKOFM_READY /tmp/l)"
/test/bin/httpget http://127.0.0.1:8326/v1/models K > /dev/null
/test/bin/httpget http://127.0.0.1:8326/v1/models K > /dev/null
echo "=== state with -body ==="
/test/bin/httpget http://127.0.0.1:8326/ctl/state -body
echo "=== state no key arg ==="
/test/bin/httpget http://127.0.0.1:8326/ctl/state
echo "=== grep test ==="
/test/bin/httpget http://127.0.0.1:8326/ctl/state -body > /tmp/s.out
cat /tmp/s.out
echo "hits=$(grep -o '"modelsHits":[0-9]*' /tmp/s.out | head -n 1)"
kill $PID 2>/dev/null
