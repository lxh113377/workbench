#!/usr/bin/env bash
# build-errors.sh — 只读列出某源码树全部编译错误（给 Lead 定位迁移期重复声明等）
set -uo pipefail
export PATH="$PATH:/usr/local/go/bin"
export CGO_ENABLED=0 GOFLAGS=-mod=mod GOCACHE=/tmp/gc GOPATH=/tmp/gp
SRC="${1:-/workbench}"
cd "$SRC" || { echo NO_SRC; exit 2; }
go build ./... 2>&1 | head -80
