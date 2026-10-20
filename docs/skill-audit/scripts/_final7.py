import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
c = Counter(r['overall'] for r in d['rows'])
print('gen=', d['generated_at'], dict(c))
print('GREEN=', [r['local_skill'] for r in d['rows'] if r['overall'] == 'GREEN'])
five = ('brainstorming','code-review','debugging-fixing','refactoring','security-audit')
for r in d['rows']:
    if r['local_skill'] in five:
        print('%-18s %-10s drift=%-5s main=%s %s' % (r['local_skill'], r['overall'], r['leg_drift']['state'], r.get('local_main_bytes'), r['leg_drift']['note'][:62]))