import os
p = 'D:/global_skills/A-dynamic-workflow/SKILL.md'
t = open(p, encoding='utf-8').read()
print('has 1.1.0:', 'version: 1.1.0' in t)
t = t.replace('version: 1.1.0', 'version: 1.4.0', 1)
t = t.replace('- **1.1.0** (2026-10-20 轮3)', '- **1.4.0** (2026-10-20 轮3)', 1)
open(p,'w',encoding='utf-8').write(t)
print('fixed to 1.4.0; bytes', os.path.getsize(p))