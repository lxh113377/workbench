#!/usr/bin/env bash
# capability-smoke.sh — 二进制能力面自证（CI 与本地共用；与 ability-probe.ps1 同判据、不同环境）
#
# 判据：8 个必须存在的命令面都能被识别，且不挂起。
# 不要求退 0：--soak / --acceptance 在无数据时按设计退 3（unverified），这是三态纪律。
set -uo pipefail
B="${1:-/tmp/wb}"
ok=1

probe() {
  local label="$1"; shift
  timeout 25 "$B" "$@" >/dev/null 2>&1
  local rc=$?
  echo "  $label -> rc=$rc"
  if [ "$rc" -eq 127 ]; then echo "MISSING: $label"; ok=0; fi
  if [ "$rc" -eq 124 ]; then echo "HUNG: $label"; ok=0; fi
}

probe "--version" --version
probe "--diagnose" --diagnose
probe "--status" --status
probe "--autonomy selftest" --autonomy selftest
probe "--autonomy status" --autonomy status
probe "--acceptance" --acceptance
probe "--soak" --soak
probe "--model-pool status" --model-pool status

if [ "$ok" -eq 1 ]; then echo "CAPABILITY_SMOKE_OK"; exit 0; fi
echo "CAPABILITY_SMOKE_FAIL"; exit 1
