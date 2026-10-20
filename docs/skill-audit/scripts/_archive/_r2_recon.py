import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
no_tag = [e['local_skill'] for e in reg['watchlist'] if not e.get('recorded_version')]
no_path = [e['local_skill'] for e in reg['watchlist'] if not e.get('upstream_path')]
print('entries=', len(reg['watchlist']))
print('no recorded_version:', len(no_tag), no_tag[:20])
print('no upstream_path:', len(no_path), no_path[:20])
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
from collections import Counter
print('sweep:', dict(Counter(r['overall'] for r in d['rows'])))
unv = [r for r in d['rows'] if r['overall'] == 'UNVERIFIED']
causes = Counter()
for r in unv:
    n = r['leg_version']['note']
    causes[n[:24]] += 1
print('UNVERIFIED causes:', dict(causes))