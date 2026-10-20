import json, time, urllib.request
UA='Mozilla/5.0'
# 实测 ungh.cc 当前状态：连续请求看是否真被限流
for rp in ('obra/superpowers','kepano/obsidian-skills','vercel-labs/agent-browser'):
    try:
        req = urllib.request.Request('https://ungh.cc/repos/' + rp, headers={'User-Agent': UA})
        d = json.loads(urllib.request.urlopen(req, timeout=15).read().decode('utf-8','replace'))
        r = d.get('repo') or {}
        print('%-30s stars=%s' % (rp, r.get('stars')))
    except Exception as e:
        print('%-30s ERR %s' % (rp, repr(e)[:60]))
    time.sleep(1.0)