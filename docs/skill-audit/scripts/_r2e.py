import urllib.request, re
UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
for rp in ('genspark-ai/genoffice','obra/superpowers','ZSeven-W/openpencil'):
    try:
        req = urllib.request.Request('https://github.com/'+rp+'/tree/HEAD/skills', headers={'User-Agent':UA,'Accept':'text/html'})
        h = urllib.request.urlopen(req, timeout=25).read().decode('utf-8','replace')
        hrefs = re.findall(r'href="/'+re.escape(rp)+r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
        print(rp, 'len(html)=', len(h), 'hrefs=', len(hrefs), 'sample=', [p for _,p in hrefs[:5]])
    except Exception as e:
        print(rp, 'ERR', repr(e)[:70])
# also test raw fixed path directly
for rp, path in (('genspark-ai/genoffice','SKILL.md'), ('obra/superpowers','skills/executing-plans/SKILL.md')):
    try:
        u = 'https://raw.githubusercontent.com/'+rp+'/HEAD/'+path
        b = urllib.request.urlopen(urllib.request.Request(u, headers={'User-Agent':UA}), timeout=20).read()
        print('RAW OK', rp, path, len(b))
    except Exception as e:
        print('RAW ERR', rp, path, repr(e)[:50])