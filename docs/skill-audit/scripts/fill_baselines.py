import json, urllib.request, re, time, datetime
UA = 'Mozilla/5.0'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
need = [e for e in reg['watchlist'] if not e.get('recorded_version') and e.get('upstream_repo')]
out = {'ran_at': datetime.datetime.now().isoformat(timespec='seconds'), 'filled': [], 'failed': []}
for e in need:
    rp = e['upstream_repo']
    tag = None
    try:
        req = urllib.request.Request('https://github.com/' + rp + '/releases.atom', headers={'User-Agent': UA})
        xml = urllib.request.urlopen(req, timeout=20).read().decode('utf-8', 'replace')
        m = re.search(r'<entry>.*?<title>(.*?)</title>', xml, re.S)
        tag = m.group(1).strip() if m else None
    except Exception as ex:
        out['failed'].append({'local_skill': e['local_skill'], 'repo': rp, 'err': repr(ex)[:60]})
    if tag:
        e['recorded_version'] = tag
        e['last_checked'] = '2026-10-20'
        out['filled'].append({'local_skill': e['local_skill'], 'repo': rp, 'tag': tag})
    time.sleep(0.6)
json.dump(reg, open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/baseline-fill-run-2026-10-20.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print('filled=%d failed=%d' % (len(out['filled']), len(out['failed'])))
for f in out['failed']: print('  FAIL', f['local_skill'], f['repo'], f['err'][:50])