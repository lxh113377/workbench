#!/usr/bin/env bash
# probe-build.sh — 只读探测任意源码树能否编译（不修改该树；用 -mod=mod 且禁写回）
# 用法: SRC_DIR=/src/xxx bash /dk/probe-build.sh
# 判据：输出 BUILD_OK / BUILD_FAIL 与首个编译错误（不猜、不吞）
export PATH="$PATH:/usr/local/go/bin"
export CGO_ENABLED=0
SRC_DIR="${SRC_DIR:-/src/workbench}"
echo "SRC=$SRC_DIR"
# go.mod BOM 检测：有 BOM 会让 go 报 unexpected input character
if [ -f "$SRC_DIR/go.mod" ]; then
  head -c 3 "$SRC_DIR/go.mod" | od -An -tx1 | tr -d ' \n' | sed 's/^/go.mod first3: /'
  echo
fi
cd "$SRC_DIR" || { echo "NO_SRC"; exit 2; }
# 用独立 GOCACHE/GOMODCACHE 避免污染，-mod=mod 允许自动修 go.sum
export GOCACHE=/tmp/gocache GOFLAGS=-mod=mod GOPATH=/tmp/gopath
out=$(go build ./... 2>&1); rc=$?
if [ "$rc" -eq 0 ]; then
  echo "BUILD_OK"
else
  echo "BUILD_FAIL"
  echo "$out" | head -40
fi
