import json, os, re, time, urllib.parse, urllib.request, datetime
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) batchC-search/1.0'
URL='https://api.github.com/search/repositories?q={0}&sort=stars&order=desc&per_page=5'
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
have = set(e['local_skill'] for e in reg['watchlist'])
todo = [i for i in inv['items'] if i.get('has_skill_md') and i['name'] not in have]
# desc map from skill_content data layer
descs = {}
import glob
for f in glob.glob(r'D:/global_memory/skill_content/*.json'):
    try: d = json.load(open(f, encoding='utf-8'))
    except Exception: continue
    items = d if isinstance(d, list) else d.get('skills', []) if isinstance(d, dict) else []
    for s in items:
        if isinstance(s, dict) and s.get('name'): descs[s['name']] = s.get('desc') or ''
def query_for(name):
    dt = descs.get(name, '')
    en = re.findall(r'[A-Za-z][A-Za-z0-9.+#-]{3,}', dt)
    stop = set('use when skill skills user users the and for with this that from are you your agent claude code github new also must before after'.split())
    picks = []
    for w in en:
        lw = w.lower()
        if lw in stop or lw in picks: continue
        picks.append(lw)
        if len(picks) >= 4: break
    core = ' '.join(picks) if picks else name.replace('-', ' ')
    return (core + ' skill agent').strip()
out = {'schema':'batchC-search-evidence-v1','started':datetime.datetime.now().isoformat(timespec='seconds'),'queries':[]}
ok = err = 0
for i, e in enumerate(todo):
    q = query_for(e['name'])
    rec = {'local_skill': e['name'], 'query': q, 'bytes': e.get('skill_md_bytes')}
    try:
        req = urllib.request.Request(URL.format(urllib.parse.quote(q)), headers={'User-Agent': UA, 'Accept': 'application/vnd.github+json'})
        with urllib.request.urlopen(req, timeout=25) as r:
            d = json.loads(r.read().decode('utf-8','replace'))
        rec['total_count'] = d.get('total_count')
        cands = []
        for it in (d.get('items') or []):
            cands.append({'repo': it.get('full_name'), 'stars': it.get('stargazers_count'), 'pushed': str(it.get('pushed_at'))[:10], 'license': (it.get('license') or {}).get('spdx_id'), 'archived': it.get('archived')})
        rec['top'] = cands
        ok += 1
    except urllib.error.HTTPError as he:
        rec['error'] = 'HTTP %s' % he.code; err += 1
        if he.code in (403, 422):
            out['aborted_at'] = i; out['queries'].append(rec); break
    except Exception as ex:
        rec['error'] = repr(ex)[:80]; err += 1
    out['queries'].append(rec)
    if (i+1) % 10 == 0:
        out['progress'] = i + 1
        json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/batchC-progress.json','w',encoding='utf-8'), ensure_ascii=False, indent=1)
    time.sleep(6.5)
out['ok'] = ok; out['err'] = err; out['total_todo'] = len(todo)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/batchC-search-evidence.json','w',encoding='utf-8'), ensure_ascii=False, indent=1)
print('done ok=%d err=%d todo=%d' % (ok, err, len(todo)))