import urllib.request
UA='Mozilla/5.0'
cases = [
  ('A-memory-start','yonatangross/orchestkit',None),
  ('audit-runner-safe-aggregate','skills-lock/skil-lock',None),
  ('video-frames','JimmySadek/video-fetcher-to-markdown',None),
]
import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
for n, rp, _ in cases:
    e = ent.get(n, {})
    path = e.get('upstream_path')
    print('===', n, '->', rp, '| path=', path)
    print('    role=', (e.get('upstream_role') or '')[:70])
    print('    notes=', (e.get('notes') or '')[:70])
    if path:
        try:
            u = 'https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path.lstrip('/'))
            b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=20).read().decode('utf-8','replace')
            head = b[:300].replace(chr(10),' / ')
            print('    upstream head:', head[:200])
        except Exception as ex:
            print('    raw ERR', repr(ex)[:50])