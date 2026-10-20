import json, urllib.request, re
UA='Mozilla/5.0'
def atom_tag(repo):
    try:
        req=urllib.request.Request('https://github.com/'+repo+'/releases.atom',headers={'User-Agent':UA})
        xml=urllib.request.urlopen(req,timeout=20).read().decode('utf-8','replace')
        m=re.search(r'<entry>.*?<title>(.*?)</title>',xml,re.S)
        return m.group(1).strip() if m else None
    except Exception:
        return None
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill']=='dogfood':
        e['upstream_repo']='vercel-labs/agent-browser'
        e['upstream_path']='skill-data/dogfood/SKILL.md'
        e['upstream_role']='true_same_origin_upstream(修正0852: 自动选品误选 browser-use/qa 相邻件)'
        e['recorded_version']=atom_tag('vercel-labs/agent-browser')
        e['last_checked']='2026-10-20'
        print('dogfood upstream fixed, tag=', e['recorded_version'])
json.dump(reg, open(p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)