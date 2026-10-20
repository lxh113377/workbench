import urllib.request
UA='Mozilla/5.0'
for tag, path in [('dbg','skills/debugging-and-error-recovery/SKILL.md'), ('simp','skills/code-simplification/SKILL.md')]:
    u='https://raw.githubusercontent.com/addyosmani/agent-skills/HEAD/'+path
    b=urllib.request.urlopen(urllib.request.Request(u,headers={'User-Agent':UA}),timeout=30).read()
    open(r'D:/workdesktop/docs/skill-audit/data/_upstream_'+tag+'.md','wb').write(b)
    print(tag,'bytes',len(b))