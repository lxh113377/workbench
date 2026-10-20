import urllib.request, re
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
def get(u):
    return urllib.request.urlopen(urllib.request.Request(u,headers={'User-Agent':UA}),timeout=25).read().decode('utf-8','replace')
for u in ['https://github.com/anthropics/skills/tree/HEAD/skills','https://github.com/obra/superpowers/tree/HEAD/skills']:
    h=get(u)
    print('###',u,len(h))
    hs=sorted(set(re.findall(r'href="(/[^"]*(?:tree|blob)/[^"]*)"',h)))
    print(len(hs))
    for x in hs[:40]: print('   ',x)