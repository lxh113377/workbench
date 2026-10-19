#!/usr/bin/env bash
# consistency-scan.sh — 枚举"分发器 vs 常量"一致性扫描（整仓级，纯静态，低噪声）
#
# 由来：2026-10-19 容器验收抓到 CriterionVerified"声明了却没实现"（落 default 恒 false，
# 目标永不可完成）。那是一个**缺陷类**：同一枚举的部分常量在 switch 里有分支，另一些没有。
#
# 判据设计（关键：只报"确有分发器却漏项"的枚举，避免把 ==/map 消费误判成漏实现）：
#   对每个枚举常量族（同一文件里 `TypeXxx Type = "值"` 形态）：
#     1) 统计全仓以 `case <常量>:` 出现的该族常量数 N_case；
#     2) N_case == 0  → 该枚举靠 ==/map 消费，**不报**（这是最常见的正常形态）；
#     3) N_case >  0  → 存在 switch 分发器；此时族内**没有**任何 case 的常量 = 漏项，报 WARN。
#
# 首版（只查"有无 case"）噪声 70 条、全是误报，已废弃；本版按上述收窄。
# 用法: bash /dk/consistency-scan.sh /src/workbench
set -uo pipefail
SRC="${1:-/workbench}"

echo "=== 枚举族：分发器存在性 vs 成员覆盖 ==="
warn=0
for f in $(find "$SRC" -name "*.go" -not -name "*_test.go" | sort); do
  # 该文件里所有 "标识符 类型名 = \"字符串值\"" 形态的常量：类型名 + 常量名
  awk '/^[[:space:]]*[A-Z][A-Za-z0-9_]*[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*"/{print $2" "$1}' "$f" \
    | sort -u \
    | while read -r tname cname; do
        [ -z "$tname" ] && continue
        # 跳过明显非枚举类型名（避免把 `key string = "value"` 这类当枚举）
        case "$tname" in *Kind|*State|*Mode|*Level|*Phase|*Action|*Lane|*Format|*Strategy|*Risk|*Status|*Dir|*Op) ;; *) continue ;; esac
        # 族内成员数：同文件同类型名的常量数
        members=$(awk -v t="$tname" '$2==t' <<< "$(awk '/^[[:space:]]*[A-Z][A-Za-z0-9_]*[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*"/{print $2" "$1}' "$f")" | wc -l)
        [ "$members" -lt 2 ] && continue
        # 该成员是否被任何 case 处理过
        if grep -rq --include=*.go "case $cname:" "$SRC" 2>/dev/null; then continue; fi
        # 本族是否有分发器：族内任一成员被 case 处理
        disp=0
        for m in $(awk -v t="$tname" '$2==t' <<< "$(awk '/^[[:space:]]*[A-Z][A-Za-z0-9_]*[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*"/{print $2" "$1}' "$f")" | awk '{print $1}'); do
          if grep -rq --include=*.go "case $m:" "$SRC" 2>/dev/null; then disp=1; break; fi
        done
        if [ "$disp" -eq 1 ]; then
          echo "  WARN: $tname 族存在 switch 分发器，但成员 $cname 无任何 case 分支（疑似漏实现）"
        fi
      done
done

echo "=== autonomy 判据专项（声明数 vs acceptance 处理数）==="
T="$SRC/internal/autonomy/types.go"
A="$SRC/internal/autonomy/acceptance.go"
if [ -f "$T" ] && [ -f "$A" ]; then
  d=$(awk '/CriterionKind = "/{print $1}' "$T" | sort -u | wc -l)
  h=$(grep -c 'case Criterion[A-Za-z]*:' "$A")
  echo "  CriterionKind 声明 $d 个 / acceptance 处理 $h 个"
  if [ "$d" -ne "$h" ]; then echo "  WARN: 声明数与处理数不一致 ⇒ 有判据会被 default 按未通过处理"; fi
fi

echo "CONSISTENCY_SCAN_DONE"
