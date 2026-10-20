import sys
sys.path.insert(0, r'D:/workdesktop/docs/skill-audit/scripts')
import importlib.util
spec = importlib.util.spec_from_file_location('m', r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py')
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
import json, urllib.request
UA='Mozilla/5.0'
for n, rp, path in (('test-driven-development','addyosmani/agent-skills','skills/test-driven-development/SKILL.md'),
                    ('first-principles-decomposer','addyosmani/agent-skills','skills/doubt-driven-development/SKILL.md')):
    u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path)
    up = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
    lo, mb, note = m.load_local_merged(__import__('pathlib').Path('D:/global_skills/'+n+'/SKILL.md'))
    lb = len(lo.encode()); ub = len(up.encode())
    missing = sorted(m.heading_set(up) - m.heading_set(lo))
    mn = sorted(m.heading_set(up, True) - m.heading_set(lo, True))
    print('===', n)
    print('  lb=%d ub=%d  lb<0.6ub? %s  missing_norm=%d' % (lb, ub, lb < 0.6*ub, len(mn)))
    print('  missing_norm list:', mn)