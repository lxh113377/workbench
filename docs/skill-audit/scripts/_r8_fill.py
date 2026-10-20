import json, urllib.request, re, time
UA = 'Mozilla/5.0'
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
need = [e for e in reg['watchlist'] if e.get('upstream_repo') and not e.get('recorded_version')]
print('need baseline:', [e['local_skill'] for e in need])
out = {'filled': [], 'failed': []}
for e in need:
    rp = e['upstream_repo']
    tag, src = None, None
    try:
        req = urllib.request.Request('https://github.com/' + rp + '/releases.atom', headers={'User-Agent': UA})
        xml = urllib.request.urlopen(req, timeout=20).read().decode('utf-8', 'replace')
        m = re.search(r'<entry>.*?<title>(.*?)</title>', xml, re.S)
        if m: tag, src = m.group(1).strip(), 'releases.atom'
    except Exception:
        pass
    if not tag:
        try:
            req = urllib.request.Request('https://github.com/' + rp + '/commits.atom', headers={'User-Agent': UA})
            xml = urllib.request.urlopen(req, timeout=20).read().decode('utf-8', 'replace')
            m = re.search(r'<entry>.*?<updated>(.*?)</updated>', xml, re.S)
            if m: tag, src = 'sha-HEAD@' + m.group(1)[:10], 'commits.atom'
        except Exception as ex:
            out['failed'].append({'local_skill': e['local_skill'], 'repo': rp, 'err': repr(ex)[:50]})
    if tag:
        e['recorded_version'] = tag
        e['baseline_kind'] = 'release-tag' if src == 'releases.atom' else 'content-snapshot(commits.atom 最近提交日)'
        e['last_checked'] = '2026-10-20'
        out['filled'].append({'local_skill': e['local_skill'], 'repo': rp, 'tag': tag, 'src': src})
    time.sleep(0.6)
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
json.dump(out, open(r'D:/workdesktop/docs/skill-audit/data/baseline-fill-run-2026-10-20-r8.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print('filled:', len(out['filled']), 'failed:', len(out['failed']))
for f in out['filled']: print('   %-26s %-34s %s (%s)' % (f['local_skill'], f['repo'], f['tag'][:40], f['src']))
for f in out['failed']: print('   FAIL', f['local_skill'], f['repo'], f['err'][:40])