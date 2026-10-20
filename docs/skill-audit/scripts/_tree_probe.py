import re, urllib.request
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
def get(url):
    req=urllib.request.Request(url, headers={'User-Agent':UA,'Accept':'text/html'})
    return urllib.request.urlopen(req, timeout=25).read().decode('utf-8','replace')
for rp in ('genspark-ai/genoffice','kepano/obsidian-skills','zarazhangrui/frontend-slides'):
    try:
        h=get('https://github.com/'+rp+'/tree/HEAD/skills')
        hrefs=re.findall(r'href="/'+re.escape(rp)+r'/tree/([0-9a-f]{7,40})/skills/([^"/]+)/"', h)
        print(rp,'-> tree subdirs:', sorted(set(x[1] for x in hrefs))[:10])
    except Exception as e:
        print(rp,'tree ERR',repr(e)[:60])