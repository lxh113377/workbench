import json, urllib.request, hashlib, datetime
UA='Mozilla/5.0'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
need = [e for e in reg['watchlist'] if not e.get('recorded_version') and e.get('upstream_repo') and e.get('upstream_path')]
print('candidates (repo+path):', len(need))
out = {'ran_at': datetime.datetime.now().isoformat(timespec='seconds'), 'snapshot_baselines': [], 'unresolvable': []}
for e in need:
    rp, path = e['upstream_repo'], e['upstream_path']
    try:
        u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path.lstrip('/'))
        b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent': UA}), timeout=25).read()
        sha = hashlib.sha256(b).hexdigest()[:12]
        e['recorded_version'] = 'sha256:%s@%dB' % (sha, len(b))
        e['baseline_kind'] = 'content-snapshot(无 release 通道的合集仓)'
        e['last_checked'] = '2026-10-20'
        out['snapshot_baselines'].append({'local_skill': e['local_skill'], 'repo': rp, 'path': path, 'sha': sha, 'bytes': len(b)})
    except Exception as ex:
        out['unresolvable'].append({'local_skill': e['local_skill'], 'repo': rp, 'path': path, 'err': repr(ex)[:60]})
json.dump(reg, open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/baseline-fill-run-2026-10-20.json','w',encoding='utf-8'), ensure_ascii=False, indent=1)
print('snapshot=%d unresolvable=%d' % (len(out['snapshot_baselines']), len(out['unresolvable'])))
for u in out['unresolvable']: print('  UNRES', u['local_skill'], u['repo'], u['err'][:40])