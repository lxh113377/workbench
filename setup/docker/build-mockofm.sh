#!/usr/bin/env bash
# build-mockofm.sh — 在 golang 容器内编译 mock 网关为静态 Linux 二进制
set -euo pipefail
export PATH="$PATH:/usr/local/go/bin"
export CGO_ENABLED=0
export GOFLAGS=-mod=mod
cd /dk/mockofm
go build -trimpath -o /out/mockofm .
ls -l /out/mockofm
echo "MOCKOFM_BUILD_OK"
