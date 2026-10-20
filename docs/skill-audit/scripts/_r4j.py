import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
red = [r['local_skill'] for r in d['rows'] if r['overall']=='RED']
print('RED:', red)
for n in ('test-driven-development','first-principles-decomposer'):
    r = next((x for x in d['rows'] if x['local_skill']==n), None)
    if r: print('%-30s %-11s drift=%s' % (n, r['overall'], r['leg_drift']['state']))