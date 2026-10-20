import urllib.request, re
UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
rp = 'genspark-ai/genoffice'
req = urllib.request.Request('https://github.com/'+rp+'/tree/HEAD/skills', headers={'User-Agent':UA,'Accept':'text/html'})
h = urllib.request.urlopen(req, timeout=25).read().decode('utf-8','replace')
hrefs = re.findall(r'href="/'+re.escape(rp)+r'/tree/([0-9a-f]{7,40})/([^"]*)"', h)
print('raw pairs:', hrefs[:4])
sha = hrefs[0][0]
print('first sha:', sha, 'len:', len(sha))
for s, p in hrefs[:6]:
    print(' s==sha?', s == sha, '| p=', p)