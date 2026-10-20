import urllib.request, hashlib
UA='Mozilla/5.0'
for tag, rp, path in [('prove-before-done','khaichonggg/prove-before-done','SKILL.md'), ('dogfood-skill','iizcm/dogfood-skill','SKILL.md'), ('genoffice','genspark-ai/genoffice','SKILL.md')]:
    u='https://raw.githubusercontent.com/%s/HEAD/%s' % (rp, path)
    try:
        b=urllib.request.urlopen(urllib.request.Request(u,headers={'User-Agent':UA}),timeout=25).read()
        head=b[:200].decode('utf-8','replace').replace(chr(10),' / ')[:120]
        print('%-16s 200 bytes=%d sha=%s head=%s' % (tag, len(b), hashlib.sha256(b).hexdigest()[:10], head))
    except Exception as e:
        print(tag, 'FAIL', repr(e)[:80])