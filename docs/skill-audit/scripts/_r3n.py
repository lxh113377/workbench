import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
print('UNVERIFIED:', len(unv))
for r in unv:
    v = r.get('leg_version', {}); c = r.get('leg_candidates', {})
    print('  %-28s ver=%-11s %s' % (r['local_skill'], v.get('state'), str(v.get('note'))[:56]))
    print('       cand=%-11s %s' % (c.get('state'), str(c.get('note'))[:56]))