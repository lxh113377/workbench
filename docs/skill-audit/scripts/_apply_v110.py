import subprocess, sys
SRC = r'D:/workdesktop/xtmp/_gate_head.py'   # HEAD 基线（已解码落盘）
DST = r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py'
t = open(SRC, encoding='utf-8').read()
hits = []

# 补丁 1：RE_HIT 状态枚举增「需裁决」，证据键名增「裁决单」
o1 = '| 状态=(已闭环|未闭环)\\((证据|原因)=(.+)\\)$'
n1 = '| 状态=(已闭环|需裁决|未闭环)\\((证据|裁决单|原因)=(.+)\\)$'
assert o1 in t, 'P1 anchor miss'
t = t.replace(o1, n1, 1); hits.append('P1-RE_HIT')

# 补丁 2：版本头 V1.10 记录
o2 = 'V1.9（2026-10-20）：闭环证据两铁律'
n2 = 'V1.10（2026-10-20，老大轮1立规「需我决策的都自行决策」）：① 状态枚举增「需裁决」——合法出口，必须给裁决单路径（当日 GM 日志 §裁决单 或 meta/backlog），门禁校验路径存在且含「选项/代价/建议」三件套；② 「未闭环(原因=待…/待下轮/待老大/建议回写/待确认/等提醒)」类模糊挂起由告警升为 fail。出口只有两种：已闭环（受管根证据）或需裁决（裁决单）；\n      V1.9（2026-10-20）：闭环证据两铁律'
assert o2 in t, 'P2 anchor miss'
t = t.replace(o2, n2, 1); hits.append('P2-version-note')

# 补丁 3：validate() 内状态判据块
o3 = '        if state == "未闭环" and not val:\n            errs.append("未闭环原因为空")'
n3 = ('        if state == "需裁决":\n'
      '            # V1.10：需裁决=合法出口，但必须给裁决单路径且该文件含「选项/代价/建议」三件套\n'
      '            if not val:\n'
      '                errs.append("需裁决未给裁决单路径")\n'
      '            else:\n'
      '                _vp = val.split("#", 1)[0].strip()\n'
      '                if not os.path.isfile(_vp):\n'
      '                    errs.append(f"裁决单不存在: {_vp!r}")\n'
      '                else:\n'
      '                    _vt = open(_vp, encoding="utf-8", errors="replace").read(20000)\n'
      '                    _miss = [k for k in ("选项", "代价", "建议") if k not in _vt]\n'
      '                    if _miss:\n'
      '                        errs.append(f"裁决单缺三件套要素: {_miss}（{_vp}）")\n'
      '        if state == "未闭环" and not val:\n'
      '            errs.append("未闭环原因为空")\n'
      '        # V1.10：模糊挂起硬拦——「待补/待下轮/待老大/建议回写」不是合法出口（老大轮1立规：自行决策）\n'
      '        if state == "未闭环" and val and any(k in val for k in ("待补", "待下轮", "待老大", "建议回写", "待确认", "等提醒")):\n'
      '            errs.append(f"模糊挂起非法: {val!r}（合法出口只有 已闭环 或 需裁决+裁决单；老大 2026-10-20 立规）")')
assert o3 in t, 'P3 anchor miss'
t = t.replace(o3, n3, 1); hits.append('P3-judge')

open(DST, 'w', encoding='utf-8', newline='').write(t)
print('applied:', hits, '| bytes:', len(t))