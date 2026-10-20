import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    n = e['local_skill']
    if n == 'dogfood':
        print('dogfood now:', e.get('upstream_repo'), '|', e.get('upstream_path'), '| role=', (e.get('upstream_role') or '')[:40])
    if n == 'electron':
        print('electron now:', e.get('upstream_repo'), '|', e.get('upstream_path'), '| role=', (e.get('upstream_role') or '')[:40])
    if n == 'security-audit':
        print('sec now:', e.get('upstream_repo'), '|', e.get('upstream_path'), '| tag=', e.get('recorded_version'))