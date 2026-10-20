import json, re
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
for n in ('test-driven-development','first-principles-decomposer','A-dynamic-workflow','frontend-slides'):
    r = rows.get(n)
    if not r: continue
    print('===', n)
    print('   ', r['leg_drift']['note'][:200])