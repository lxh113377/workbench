import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
kind = {e['local_skill']: (e.get('entry_kind') or '')[:18] for e in reg['watchlist']}
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED' and kind.get(r['local_skill'],'').startswith('candidate_pool')]
print('candidate_pool still UNVERIFIED:', len(unv))
for r in unv:
    print('%-24s drift=%-11s ver=%-11s cand=%s' % (r['local_skill'], r['leg_drift']['state'], r['leg_version']['state'], r['leg_candidates']['state']))
    print('    drift note:', r['leg_drift']['note'][:70])
    print('    cand  note:', r['leg_candidates']['note'][:70])