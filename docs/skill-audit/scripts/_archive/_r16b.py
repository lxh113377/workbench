import json
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
for n in ('story','story-cover','story-long-write'):
    r = rows[n]
    print('===', n, '|', r['overall'])
    for k in r:
        if k.startswith('leg_') and r[k]['state'] != 'GREEN':
            print('   %-16s %-11s %s' % (k, r[k]['state'], r[k]['note'][:90]))
            for det in (r[k].get('detail') or [])[:6]:
                if isinstance(det, dict): print('      -', det.get('repo'), det.get('state'), str(det.get('note'))[:46])
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
print()
for n in ('story','story-cover','story-long-write'):
    e = ent[n]
    print(n, '| kind:', (e.get('entry_kind') or 'NONE')[:22], '| repo:', e.get('upstream_repo'), '| tag:', (e.get('recorded_version') or '')[:30])
    print('   notes tail:', (e.get('notes') or '')[-90:])