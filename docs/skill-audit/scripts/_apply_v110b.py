t = open(r'D:/workdesktop/xtmp/_gate_head.py', encoding='utf-8').read()
DST = r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py'
hits = []

# P1: RE_HIT 状态枚举 + 证据键名
o1 = '| 状态=(已闭环|未闭环)\\((证据|原因)=(.+)\\)$'
n1 = '| 状态=(已闭环|需裁决|未闭环)\\((证据|裁决单|原因)=(.+)\\)$'
assert o1 in t, 'P1 miss'
t = t.replace(o1, n1, 1); hits.append('P1')

# P2: 版本头（HEAD 无 V1.9 行，加在「新增校验」句后）
o2 = '      新增校验：命中行的 `对照` 字段（`已取` 须带证据 / `未取` 须含「待证伪」）。'
n2 = o2 + '\n      V1.10（2026-10-20，老大轮1立规「需我决策的都自行决策」）：① 状态枚举增「需裁决」——合法出口，必须给裁决单路径（当日 GM 日志 §裁决单 或 meta/backlog），门禁校验该路径存在且含「选项/代价/建议」三件套；② 「未闭环(原因=待…/待下轮/待老大/建议回写/待确认/等提醒)」类模糊挂起由告警升为 fail。出口只有两种：已闭环（受管根证据）或需裁决（裁决单）。'
assert o2 in t, 'P2 miss'
t = t.replace(o2, n2, 1); hits.append('P2')

# P3: validate() 状态判据
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
      '        # V1.10：模糊挂起硬拦（老大轮1立规：需决策的自行决策，禁止「待下轮/待老大/建议回写」式挂起）\n'
      '        if state == "未闭环" and val and any(k in val for k in ("待补", "待下轮", "待老大", "建议回写", "待确认", "等提醒")):\n'
      '            errs.append(f"模糊挂起非法: {val!r}（合法出口只有 已闭环 或 需裁决+裁决单）")')
assert o3 in t, 'P3 miss'
t = t.replace(o3, n3, 1); hits.append('P3')

import ast
ast.parse(t)
open(DST, 'w', encoding='utf-8', newline='').write(t)
print('OK hits=', hits, 'bytes=', len(t))