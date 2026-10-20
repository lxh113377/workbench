import json, urllib.request
UA='Mozilla/5.0'
# skills-lock/skil-lock：ungh 无数据，试 api.github.com（只读公开仓信息，1 次调用）
for rp in ('skills-lock/skil-lock',):
    got = None
    try:
        req = urllib.request.Request('https://ungh.cc/repos/' + rp, headers={'User-Agent': UA})
        raw = urllib.request.urlopen(req, timeout=15).read().decode('utf-8','replace')
        print('ungh raw:', raw[:200])
    except Exception as e:
        print('ungh ERR', repr(e)[:60])
    try:
        req = urllib.request.Request('https://api.github.com/repos/' + rp, headers={'User-Agent': UA, 'Accept': 'application/vnd.github+json'})
        d = json.loads(urllib.request.urlopen(req, timeout=15).read().decode('utf-8','replace'))
        got = d.get('stargazers_count')
        print('api stars:', got, '| pushed:', d.get('pushed_at'), '| lic:', (d.get('license') or {}).get('spdx_id'))
    except Exception as e:
        print('api ERR', repr(e)[:60])