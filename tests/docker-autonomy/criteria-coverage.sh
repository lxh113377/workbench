#!/usr/bin/env bash
# criteria-coverage.sh — 验收判据"声明 vs 实现"交叉校验（静态，秒级）
#
# 为什么需要（2026-10-19 实测驱动的缺陷类别）：
#   恢复出的源码出现"混合版本树"：types.go 声明了 CriterionVerified / CriterionVerifiedRatio，
#   planner.go 也照此生成目标判据，但 acceptance.go 的 evalCriterion switch 里**没有对应分支**，
#   于是落进 default → "未知判据类型，按未通过处理" → 该判据恒 false →
#   目标永远无法通过验收 → goalDone/nextGoal 永不出现 → 最终被预算闸门 block。
#   容器实测证据：goals.json 的 criteria[].detail 字面写着「未知判据类型，按未通过处理」。
#
# 这道校验能在编译前抓住"声明了判据却没实现判定"的整类缺陷，且不依赖运行模型。
#
# 用法: bash /dk/criteria-coverage.sh /src/workbench
set -uo pipefail
SRC="${1:-/workbench}"
TYPES="$SRC/internal/autonomy/types.go"
ACC="$SRC/internal/autonomy/acceptance.go"
PLANNER="$SRC/internal/autonomy/planner.go"

for f in "$TYPES" "$ACC" "$PLANNER"; do
  if [ ! -f "$f" ]; then echo "MISSING_FILE: $f"; exit 3; fi
done

# 1) types.go 里声明的 CriterionKind 常量值（形如 CriterionXxx CriterionKind = "xxx"）
# 只认"常量声明"：行内含 CriterionKind = "..." 的左侧标识符；
# 直接 grep Criterion[A-Za-z]+ 会把**类型名** CriterionKind 本身也当判据（实测误报）。
# 逐行取：每行形如 `	CriterionXxx CriterionKind = "值"`，
# 用 awk 按空白切第一列，避免 grep -oE 在不带换行的实现下把多行粘成一个 token（实测踩到）。
declared=$(awk '/CriterionKind = "/{print $1}' "$TYPES" | sort -u)
# 2) acceptance.go 的 evalCriterion 里实际处理的分支（case CriterionXxx:）
handled=$(grep -oE 'case Criterion[A-Za-z]+:' "$ACC" | sed 's/case //; s/://' | sort -u)
# 3) planner.go 里真正会被生成到目标上的判据种类
emitted=$(grep -oE 'Kind: Criterion[A-Za-z]+' "$PLANNER" | sed 's/Kind: //' | sort -u)

echo "declared: $(echo "$declared" | tr '\n' ' ')"
echo "handled : $(echo "$handled" | tr '\n' ' ')"
echo "emitted : $(echo "$emitted" | tr '\n' ' ')"

fail=0

# 判据 A：凡是 planner 会生成的判据，acceptance 必须有处理分支
for k in $emitted; do
  if ! echo "$handled" | grep -qx "$k"; then
    echo "UNHANDLED_EMITTED: $k —— planner 会生成它，但 acceptance 没有判定分支"
    echo "  → 后果：该判据落进 default 被按「未通过」处理，目标永远无法完成"
    fail=1
  fi
done

# 判据 B：types.go 声明的判据（manual 除外，它按设计不参与自动判定）也不应有漏网
for k in $declared; do
  case "$k" in CriterionManual) continue ;; esac
  if ! echo "$handled" | grep -qx "$k"; then
    echo "UNHANDLED_DECLARED: $k —— types.go 声明了它，但 acceptance 没有判定分支"
    fail=1
  fi
done

# 判据 C：反向——handled 里的判据必须在 types.go 声明过（防拼写漂移）
for k in $handled; do
  if ! echo "$declared" | grep -qx "$k"; then
    echo "HANDLED_UNDECLARED: $k —— acceptance 处理了一个 types.go 未声明的判据（疑似拼写漂移）"
    fail=1
  fi
done

if [ "$fail" -eq 0 ]; then echo "CRITERIA_COVERAGE_OK"; exit 0; fi
echo "CRITERIA_COVERAGE_FAIL"; exit 1
