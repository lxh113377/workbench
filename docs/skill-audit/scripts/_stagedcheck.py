import json, subprocess
log = json.load(open(r'D:/workdesktop/docs/skill-audit/data/version-stamp-log.json', encoding='utf-8'))
stamped = set(log['stamped'])
d = subprocess.run(['git','-C',r'D:/global_skills','diff','--cached','--unified=0'], capture_output=True).stdout.decode('utf-8','replace')
adds = [l[1:] for l in d.splitlines() if l.startswith('+') and not l.startswith('+++')]
from collections import Counter
c = Counter(a.strip() for a in adds)
print('distinct staged additions:', dict(c))
staged_files = set()
for l in d.splitlines():
    if l.startswith('diff --git'):
        p = l.split(' b/')[-1].split('/SKILL.md')[0]
        staged_files.add(p)
print('missing from staged:', sorted(stamped - staged_files))
for m in sorted(stamped - staged_files):
    s = subprocess.run(['git','-C',r'D:/global_skills','status','--porcelain', m + '/SKILL.md'], capture_output=True).stdout.decode('utf-8','replace')
    print('   status:', repr(s))
    raw = open(r'D:/global_skills/%s/SKILL.md' % m, 'rb').read().decode('utf-8-sig', errors='replace')
    head = subprocess.run(['git','-C',r'D:/global_skills','show','HEAD:%s/SKILL.md' % m], capture_output=True).stdout.decode('utf-8','replace')
    print('   now has version:', 'version:' in raw[:1200], '| HEAD had version:', 'version:' in head[:1200])