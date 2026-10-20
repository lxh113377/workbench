import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill']=='testing':
        e['notes'] = e['notes'].replace('缺23标题中18个属浏览器域', '缺23标题实测19个属浏览器域(accessibility/available tools/console/network/perf/ui/installation/profile isolation/screenshot/security boundaries/devtools mcp/debugging workflow/untrusted data/test plans/what to look for/clean console/overview/red flags)，4个通用项(overview/verification/red flags/rationalizations)本地已有等价')
json.dump(reg, open(p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('corrected count')