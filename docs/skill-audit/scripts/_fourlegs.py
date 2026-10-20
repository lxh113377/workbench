import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], 'rows=', len(d['rows']), dict(Counter(r['overall'] for r in d['rows'])))
for r in d['rows']:
    if r['local_skill'] in ('dogfood','electron','security-audit','agent-browser'):
        print('%-16s %-11s drift=%-5s ver=%-11s %s' % (r['local_skill'], r['overall'], r['leg_drift']['state'], r['leg_version']['state'], r['leg_drift']['note'][:80]))