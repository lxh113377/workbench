import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], 'rows=', len(d['rows']), dict(Counter(r['overall'] for r in d['rows'])))
absorbed = {'brainstorming','code-review','debugging-fixing','refactoring','security-audit','writing-plans','verification-before-completion','dogfood','electron','agent-browser'}
bad = [(r['local_skill'], r['leg_drift']['state']) for r in d['rows'] if r['local_skill'] in absorbed and r['leg_drift']['state'] != 'GREEN']
print('absorbed drift-not-green:', bad)
for r in d['rows']:
    if r['local_skill'] in ('agent-browser','electron','dogfood','security-audit'):
        print('%-14s %-11s drift=%s ver=%s' % (r['local_skill'], r['overall'], r['leg_drift']['state'], r['leg_version']['state']))