import urllib.request, re
UA='Mozilla/5.0'
for rp in ('Shubhamsaboo/awesome-llm-apps','github/awesome-copilot','anthropics/skills','wshobson/agents','Colgate13/claude-cdp-debugger'):
    try:
        req = urllib.request.Request('https://github.com/'+rp+'/releases.atom', headers={'User-Agent':UA})
        xml = urllib.request.urlopen(req, timeout=20).read().decode('utf-8','replace')
        entries = re.findall(r'<entry>', xml)
        m = re.search(r'<entry>.*?<title>(.*?)</title>', xml, re.S)
        print('%-38s entries=%d first=%s' % (rp, len(entries), (m.group(1).strip()[:40] if m else 'NONE')))
    except Exception as e:
        print('%-38s ERR %s' % (rp, repr(e)[:60]))