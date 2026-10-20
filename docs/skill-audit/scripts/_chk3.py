import json
p=r'D:/workdesktop/docs/skill-audit/data/sweep-report.json'
d=json.load(open(p,encoding='utf-8'))
from collections import Counter
print('gen=',d['generated_at'],'rows=',len(d['rows']),dict(Counter(r['overall'] for r in d['rows'])))
for r in d['rows']:
    if r['local_skill'] in ('verification-before-completion','brainstorming'):
        print(r['local_skill'], r['overall'], '|', r['leg_drift']['note'][:110])