import json, time, urllib.parse, urllib.request, datetime
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) skill-audit-discovery/1.0'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
TARGETS = ('verification-before-completion','brainstorming','code-review','debugging-fixing','refactoring','security-audit','dogfood','testing','ci-pipeline-debugging','docx')
entries = [e for e in reg['watchlist'] if e['local_skill'] in TARGETS and e.get('discovery_query')]
url = 'https://api.github.com/search/repositories?q={0}&sort=stars&order=desc&per_page=6'
out = {'schema':'discovery-run-v1','ran_at':datetime.datetime.now().isoformat(timespec='seconds'),'entries':[]}
ok = err = 0
for e in entries:
    q = e['discovery_query']
    known = set([str(c.get('repo','')).lower() for c in (e.get('candidates') or [])] + [str(e.get('upstream_repo') or '').lower()])
    rec = {'local_skill': e['local_skill'], 'query': q, 'new_active': []}
    try:
        req = urllib.request.Request(url.format(urllib.parse.quote(q)), headers={'User-Agent': UA, 'Accept': 'application/vnd.github+json'})
        with urllib.request.urlopen(req, timeout=25) as r:
            d = json.loads(r.read().decode('utf-8','replace'))
        ok += 1
        for it in (d.get('items') or []):
            full = (it.get('full_name') or '').lower()
            pushed = str(it.get('pushed_at'))[:10]
            if not full or full in known: continue
            try: age = (datetime.date.today() - datetime.date.fromisoformat(pushed)).days
            except Exception: age = 9999
            if age > 120 or it.get('archived'): continue
            rec['new_active'].append({'repo': it.get('full_name'), 'stars': it.get('stargazers_count'), 'pushed': pushed, 'license': (it.get('license') or {}).get('spdx_id'), 'desc': (it.get('description') or '')[:110]})
    except Exception as ex:
        err += 1; rec['error'] = repr(ex)[:90]
    rec['new_active'] = rec['new_active'][:5]
    out['entries'].append(rec)
    print('%-30s %s new=%d %s' % (e['local_skill'], ('ERR' if rec.get('error') else 'OK '), len(rec['new_active']), '; '.join(x['repo'] + '(' + str(x['stars']) + ')' for x in rec['new_active'][:3])))
    time.sleep(7)
out['ok'] = ok; out['err'] = err
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/discovery-run-2026-10-20.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('ok=%d err=%d' % (ok, err))