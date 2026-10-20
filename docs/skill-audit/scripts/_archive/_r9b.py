import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
for n in ('discover-agent-cli','local-computer-use','figma','spike'):
    r = rows.get(n)
    if not r: continue
    reds = [(k, r[k]['note'][:80]) for k in r if k.startswith('leg_') and r[k]['state']=='RED']
    print('===', n, '| red legs:', [k for k,_ in reds])
    for k, note in reds: print('    ', k, note)