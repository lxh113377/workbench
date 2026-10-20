import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])), 'http=', d['http_calls'])
five = ('brainstorming','code-review','debugging-fixing','refactoring','security-audit')
for r in d['rows']:
    if r['local_skill'] in five or r['local_skill'] in ('writing-plans','verification-before-completion','dogfood','executing-plans'):
        print('%-32s %-11s drift=%s mainB=%s %s' % (r['local_skill'], r['overall'], r['leg_drift']['state'], r.get('local_main_bytes'), r['leg_drift']['note'][:70]))