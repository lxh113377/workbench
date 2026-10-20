import json, subprocess
log = json.load(open(r'D:/workdesktop/docs/skill-audit/data/version-stamp-log.json', encoding='utf-8'))
paths = ['%s/SKILL.md' % n for n in log['stamped']]
r1 = subprocess.run(['git','-C',r'D:/global_skills','add','--'] + paths, capture_output=True)
print('add rc=', r1.returncode, r1.stderr.decode('utf-8','replace')[:200])
st = subprocess.run(['git','-C',r'D:/global_skills','diff','--cached','--stat'], capture_output=True).stdout.decode('utf-8','replace')
tail = st.splitlines()
print('staged files=%d' % len([l for l in tail if '|' in l]))
print(chr(10).join(tail[-3:]))
only = subprocess.run(['git','-C',r'D:/global_skills','diff','--cached','--numstat'], capture_output=True).stdout.decode('utf-8','replace')
bad = [l for l in only.splitlines() if l.strip() and (lambda p: int(p[0]) > 2)(l.split('\t')[:1] and [int(l.split(chr(9))[0])] or [0])]
print('files adding >2 lines:', len(bad))
for l in bad[:6]: print('   ', l)