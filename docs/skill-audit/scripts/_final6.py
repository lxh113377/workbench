import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])), 'http=', d['http_calls'])
five = ('brainstorming','code-review','debugging-fixing','refactoring','security-audit')
for r in d['rows']:
    if r['local_skill'] in five:
        print('%-20s %-11s drift=%-11s mainB=%s %s' % (r['local_skill'], r['overall'], r['leg_drift']['state'], r.get('local_main_bytes'), r['leg_drift']['note'][:64]))