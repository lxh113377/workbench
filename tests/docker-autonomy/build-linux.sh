#!/usr/bin/env bash
# build-linux.sh — 在 golang:1.27-bookworm 容器内交叉编译静态 Linux 二进制
#
# 为什么要在 Linux 容器里编：宿主机是 Windows，Go 交叉编译出的 linux 产物
# 若带 CGO 会动态链接 glibc，换到 alpine(musl) 直接起不来且报错误导。
# 这里固定 CGO_ENABLED=0，并用 debug/elf 判定是否存在 PT_INTERP 做静态自检
# （golang 镜像里没有 file(1)，实测 command not found）。
set -euo pipefail
export PATH="$PATH:/usr/local/go/bin"
export CGO_ENABLED=0
export GOFLAGS=-mod=mod

SRC_DIR="${SRC_DIR:-/src/workbench}"
cd "$SRC_DIR"
go build -trimpath -ldflags "-s -w -X main.version=docker-e2e -X main.buildTime=docker" -o /out/workbench-linux-amd64 ./cmd/workbench
ls -l /out/workbench-linux-amd64

cat > /tmp/checkstatic.go <<'GOEOF'
package main

import (
	"debug/elf"
	"fmt"
	"os"
)

func main() {
	f, err := elf.Open(os.Args[1])
	if err != nil {
		fmt.Println("ELF_OPEN_FAIL:", err)
		os.Exit(2)
	}
	defer f.Close()
	for _, pr := range f.Progs {
		if pr.Type == elf.PT_INTERP {
			fmt.Println("STATIC_FAIL (has PT_INTERP)")
			os.Exit(2)
		}
	}
	fmt.Println("STATIC_OK")
}
GOEOF
go run /tmp/checkstatic.go /out/workbench-linux-amd64
echo "BUILD_PASS"
