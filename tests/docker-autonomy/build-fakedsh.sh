#!/usr/bin/env bash
# build-fakedsh.sh — 在 golang 容器里编译 DSH 替身（静态 Linux）
set -euo pipefail
export PATH="$PATH:/usr/local/go/bin"
export CGO_ENABLED=0
export GOFLAGS=-mod=mod
cd /dk/fakedsh
go build -trimpath -o /out/fakedsh .
ls -l /out/fakedsh
echo "FAKEDSH_BUILD_OK"
