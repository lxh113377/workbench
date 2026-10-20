import urllib.request, re
UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
def norm(s): return re.sub(r'[^a-z0-9]', '', (s or '').lower())
cases = [('xlsx','genspark-ai/genoffice'), ('story-long-write','danderfer/Comp_Sci_Sem_2'), ('pdf','AgriciDaniel/claude-seo')]
for name, rp in cases:
    try:
        req = urllib.request.Request('https://github.com/'+rp+'/tree/HEAD/skills', headers={'User-Agent':UA,'Accept':'text/html'})
        h = urllib.request.urlopen(req, timeout=20).read().decode('utf-8','replace')
        hrefs = re.findall(r'href="/'+re.escape(rp)+r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
        subs = []
        for s, p in hrefs:
            rest = p[len('skills/'):] if p.startswith('skills/') else p
            if rest and '/' not in rest and rest not in subs: subs.append(rest)
        print(name, '->', subs[:6], '| norm match:', [s for s in subs if norm(s)==norm(name)])
    except Exception as e:
        print(name, rp, 'ERR', repr(e)[:50])