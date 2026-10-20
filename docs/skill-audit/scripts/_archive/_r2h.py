import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
c = Counter(r['leg_version']['note'][:22] for r in unv)
print('UNVERIFIED causes now:', dict(c))
snap = [r for r in d['rows'] if r['leg_version']['state']=='GREEN' and 'sha256' in r['leg_version']['note']]
print('snapshot-baseline greens:', len(snap), [r['local_skill'] for r in snap][:14])