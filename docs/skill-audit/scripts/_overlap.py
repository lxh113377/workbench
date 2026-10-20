import json, subprocess
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
nv = set(i['dir'] for i in d['items'] if i.get('has_skill_md') and not i.get('version'))
out = subprocess.run(['git','-C',r'D:/global_skills','status','--porcelain'], capture_output=True).stdout.decode('utf-8','replace')
dirty = set()
for ln in out.splitlines():
    p = ln[3:].strip().replace(chr(92),'/')
    if p.endswith('/SKILL.md'):
        dirty.add(p.split('/')[0])
print('nover=%d dirty_skill_md=%d overlap=%d' % (len(nv), len(dirty), len(nv & dirty)))
print('overlap list:', sorted(nv & dirty))
print('nover NOT dirty:', sorted(nv - dirty)[:20])