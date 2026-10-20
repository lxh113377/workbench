import subprocess, re
head = subprocess.run(['git','-C',r'D:/global_skills','status','--porcelain'], capture_output=True).stdout.decode('utf-8','replace')
rows = [l for l in head.splitlines() if l.strip()]
kinds = {}
for l in rows:
    k = l[:2]
    kinds[k] = kinds.get(k, 0) + 1
print('dirty kinds:', kinds, 'total:', len(rows))
only_lastupd = 0
import subprocess as sp
for l in rows[:40]:
    f = l[3:].strip()
    d = sp.run(['git','-C',r'D:/global_skills','diff','--unified=0','--',f], capture_output=True).stdout.decode('utf-8','replace')
    body = [x for x in d.splitlines() if (x.startswith('+') or x.startswith('-')) and not x.startswith('+++') and not x.startswith('---')]
    if body and all(re.match(r'^[+-]last_updated: \d{4}-\d{2}-\d{2}\s*$', x) for x in body):
        only_lastupd += 1
print('first40 sampled: pure last_updated-only =', only_lastupd)