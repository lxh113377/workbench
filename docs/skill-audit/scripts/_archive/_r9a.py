import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
for n in ('discover-agent-cli','local-computer-use','figma','spike'):
    r = rows.get(n)
    if not r: continue
    print('===', n, '|', r['overall'])
    print('   cand:', r['leg_candidates']['state'], r['leg_candidates']['note'][:80])
    for det in (r['leg_candidates'].get('detail') or [])[:5]:
        print('      -', det.get('repo'), det.get('state'), str(det.get('note'))[:50])