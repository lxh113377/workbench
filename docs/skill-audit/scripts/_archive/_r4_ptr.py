import os
p = 'D:/global_skills/test-driven-development/SKILL.md'
raw = open(p,'rb').read()
bom = raw[:3] == bytes([0xEF,0xBB,0xBF])
t = raw.decode('utf-8-sig')
nl = chr(13)+chr(10) if (chr(13)+chr(10)) in t else chr(10)
lines = t.splitlines()
# bump version 1.1.0 -> 1.2.0
vi = next((i for i,l in enumerate(lines) if l.startswith('version:')), None)
if vi is not None and lines[vi].strip() == 'version: 1.1.0':
    lines[vi] = 'version: 1.2.0'
ptr = ['', '## Upstream-absorbed sections（分卷指针 · 二批）', '',
  '第二批吸收见 `references/upstream-agent-skills-tdd-part2.md`（2026-10-20 轮4：决策 guide / 五条写法判据 / 反模式 / DevTools 运行期双验证 / 派子代理时机）。Overview/When to Use/Step1-3 与主文件及卷一重复，按不重复原则未搬。']
if '## Version history' in t:
    hi = next(i for i,l in enumerate(lines) if l.startswith('## Version history'))
    lines = lines[:hi] + ptr + ['', '## Version history'] + lines[hi+1:]
else:
    lines = lines + ptr
body = nl.join(lines) + (nl if t.endswith(nl) else '')
data = body.encode('utf-8')
if bom: data = bytes([0xEF,0xBB,0xBF]) + data
open(p,'wb').write(data)
print('main bytes:', os.path.getsize(p))
print('version line:', [l for l in open(p,encoding='utf-8').read().splitlines() if l.startswith('version:')])