import json
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
items = inv['items']
withmd = [i for i in items if i.get('has_skill_md')]
print('inventory: total=%d with_md=%d over12k=%d dead=%d loose=%d declared=%d nover=%d top1=%d' % (
  len(items), len(withmd), sum(1 for i in items if (i.get('skill_md_bytes') or 0) > 12288),
  sum(1 for i in items if i.get('ref_dead')), sum(1 for i in items if i.get('ref_loose')),
  sum(1 for i in items if i.get('ref_declared')), sum(1 for i in items if not i.get('version')),
  sum(1 for i in items if i.get('route_top1'))))
print('watchlist entries:', len(reg['watchlist']))
print('matrix:', len(m['skills']), 'skills /', sum(s.get('candidate_count',0) for s in m['skills']), 'candidates')
print('sweep:', dict(Counter(r['overall'] for r in d['rows'])), 'gen=', d['generated_at'])
kinds = Counter((e.get('entry_kind') or 'same_origin')[:20] for e in reg['watchlist'])
print('entry kinds:', dict(kinds))