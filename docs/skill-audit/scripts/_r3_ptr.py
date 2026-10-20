import os, re
p = 'D:/global_skills/A-dynamic-workflow/SKILL.md'
raw = open(p,'rb').read()
bom = raw[:3] == bytes([0xEF,0xBB,0xBF])
t = raw.decode('utf-8-sig')
nl = chr(13)+chr(10) if (chr(13)+chr(10)) in t else chr(10)
lines = t.splitlines()
vi = next(i for i,l in enumerate(lines) if l.startswith('version:'))
old = lines[vi]
lines[vi] = 'version: 1.1.0'
ptr = [
  '',
  '## Upstream-absorbed sections（分卷指针）',
  '',
  '受控吸收的上游内容整卷在 `references/upstream-multi-reviewer-patterns.md`（吸收上游 wshobson/agents multi-reviewer-patterns v1.0.2，2026-10-20 轮3；含维度分配表/去重规则/严重度校准/合并报告模板四块，本地原缺）。加载本技能时一并读取。',
  '',
  '## Version history',
  '',
  '- **1.1.0** (2026-10-20 轮3) — 吸收上游 `wshobson/agents` multi-reviewer-patterns v1.0.2（12 个独有标题译写为新卷 references/upstream-multi-reviewer-patterns.md）；漂移腿 RED 处置=吸收增量（sim 0.555 同源判定成立）。',
]
if '## Version history' in t:
    hi = next(i for i,l in enumerate(lines) if l.startswith('## Version history'))
    lines = lines[:hi] + ptr[:-3] + lines[hi:]
else:
    lines = lines + ptr
body = nl.join(lines) + (nl if t.endswith(nl) else '')
data = body.encode('utf-8')
if bom: data = bytes([0xEF,0xBB,0xBF]) + data
open(p,'wb').write(data)
print('main bytes now:', os.path.getsize(p), '| old:', old, '| bom:', bom)