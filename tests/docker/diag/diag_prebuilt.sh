#!/bin/sh
# 临时诊断：10:08 预构建 linux 二进制是否含完整模型池 CLI
set -u
mkdir -p /w/root/bin
cp /d/workbench-linux-amd64 /w/root/bin/workbench-linux
chmod +x /w/root/bin/workbench-linux
export HOME=/root
cd /w/root
echo "=== version ==="
/w/root/bin/workbench-linux --version 2>&1 | head -2
echo "=== --model-pool help ==="
/w/root/bin/workbench-linux --model-pool help 2>&1 | head -14
echo "rc=$?"
echo "=== --model-pool status ==="
/w/root/bin/workbench-linux --model-pool status 2>&1 | head -6
echo "=== 便携根产物 ==="
find /w/root -type f 2>/dev/null | head -12
