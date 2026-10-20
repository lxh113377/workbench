import json
from collections import Counter
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
kind = {e['local_skill']: e.get('entry_kind','') for e in reg['watchlist']}
notes = {e['local_skill']: (e.get('notes') or '') for e in reg['watchlist']}
red = [r for r in d['rows'] if r['overall']=='RED']
print('RED:', len(red))
no_verdict = []
for r in red:
    n = r['local_skill']
    has = ('裁决=' in notes.get(n,'')) or ('裁决：' in notes.get(n,''))
    if not has: no_verdict.append((n, kind.get(n,'')[:16], r['leg_drift']['state'], r['leg_candidates']['state']))
print('RED without verdict note:', len(no_verdict))
for x in no_verdict: print('  ', x)
print()
print('RED with verdict:', len(red)-len(no_verdict))