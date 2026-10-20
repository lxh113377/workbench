import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
c = Counter(r['leg_version']['note'][:34] for r in unv)
print('UNVERIFIED:', len(unv))
for k, v in c.most_common(): print('  %2d  %s' % (v, k))