import subprocess
c = subprocess.run(['git','-C',r'D:/global_skills','diff','--cached','--name-only'], capture_output=True).stdout.decode('utf-8','replace').split()
mine = all(('/'.join(p.split('/')[:2]) in {'brainstorming/SKILL.md','brainstorming/references','code-review/SKILL.md','code-review/references','debugging-fixing/SKILL.md','debugging-fixing/references','refactoring/SKILL.md','refactoring/references','security-audit/SKILL.md','security-audit/references'}) for p in c)
print('cached files:', len(c), 'pure:', mine)
for p in c: print('  ', p)