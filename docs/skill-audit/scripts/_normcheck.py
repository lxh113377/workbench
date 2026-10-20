import importlib.util, json
spec = importlib.util.spec_from_file_location('qs', r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py')
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
import urllib.request
UA='Mozilla/5.0'
up = urllib.request.urlopen(urllib.request.Request('https://raw.githubusercontent.com/obra/superpowers/HEAD/skills/verification-before-completion/SKILL.md', headers={'User-Agent':UA}), timeout=25).read().decode('utf-8','replace')
lo = open(r'D:/global_skills/verification-before-completion/SKILL.md', encoding='utf-8').read()
missing = sorted(m.heading_set(up, True) - m.heading_set(lo, True))
print('missing_norm =', missing)
print('local_norm_count =', len(m.heading_set(lo, True)), 'upstream_norm_count =', len(m.heading_set(up, True)))