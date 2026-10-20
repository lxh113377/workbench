import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], 'rows=', len(d['rows']), dict(Counter(r['overall'] for r in d['rows'])))
absorbed = {'brainstorming','code-review','debugging-fixing','refactoring','security-audit','writing-plans','verification-before-completion','dogfood','electron'}
bad = [r for r in d['rows'] if r['local_skill'] in absorbed and r['leg_drift']['state'] != 'GREEN']
print('absorbed-but-drift-not-green:', [(r['local_skill'], r['leg_drift']['state'], r['leg_drift']['note'][:60]) for r in bad])
red_batchd = [r['local_skill'] for r in d['rows'] if r['overall'] == 'RED' and (r.get('upstream_repo') or '').count('/') and r['local_skill'] not in absorbed and r['local_skill'] not in {'testing','ci-pipeline-debugging'}]
print('other RED count:', len(red_batchd), red_batchd[:12])
unres = [r['local_skill'] for r in d['rows'] if '未登记 upstream_path' in r['leg_drift']['note'] or '取不到' in r['leg_drift']['note']]
print('drift-unverified(path unresolved):', len(unres))