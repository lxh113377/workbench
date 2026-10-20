import json, urllib.request, re
UA='Mozilla/5.0'
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
have = set(e['local_skill'] for e in reg['watchlist'])
ver = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchD-entry-verify.json', encoding='utf-8'))
added = 0
for x in ver['checked']:
    if x['local_skill'] in have: continue
    tag = None
    try:
        req = urllib.request.Request('https://github.com/' + x['repo'] + '/releases.atom', headers={'User-Agent': UA})
        xml = urllib.request.urlopen(req, timeout=15).read().decode('utf-8', 'replace')
        m = re.search(r'<entry>.*?<title>(.*?)</title>', xml, re.S)
        tag = m.group(1).strip() if m else None
    except Exception:
        tag = None
    reg['watchlist'].append({
        'local_skill': x['local_skill'],
        'local_skill_md': 'D:/global_skills/' + x['local_skill'] + '/SKILL.md',
        'upstream_repo': x['repo'],
        'upstream_path': x.get('resolved_path'),
        'upstream_role': 'discovered_strong_candidate(batch-D 2026-10-20; 入口件%s)' % ('实测200' if x.get('http') == 200 else '未定位'),
        'recorded_version': tag,
        'last_checked': '2026-10-20',
        'discovery_query': 'batch-C 提词检索',
        'candidates': [{'repo': x['repo'], 'entry_path': x.get('resolved_path'), 'stars': x.get('stars'), 'license': x.get('license'), 'score_total': None}],
        'notes': ''
    })
    added += 1
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('watchlist appended', added, 'total', len(reg['watchlist']))