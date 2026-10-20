import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
print('executing-plans in rows:', any(r['local_skill']=='executing-plans' for r in d['rows']))
print('sdd in rows:', any(r['local_skill']=='subagent-driven-development' for r in d['rows']))
r = next((x for x in d['rows'] if x['local_skill']=='subagent-driven-development'), None)
if r: print('sdd legs:', {k: r[k]['state'] for k in r if k.startswith('leg_')})