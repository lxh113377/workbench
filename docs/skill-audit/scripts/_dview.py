import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchD-entry-verify.json', encoding='utf-8'))
res = [x for x in d['checked'] if x.get('http') == 200]
un = [x for x in d['checked'] if x.get('http') != 200]
print('resolved', len(res), 'unresolved', len(un))
for x in res: print('  %-26s %-40s %-46s %sB' % (x['local_skill'], x['repo'], x['resolved_path'], x['bytes']))
print('unresolved sample tries:')
for x in un[:4]: print('  ', x['local_skill'], x['tried'][:4])