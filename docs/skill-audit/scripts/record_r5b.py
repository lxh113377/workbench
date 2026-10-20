import json
from collections import Counter
p = r'D:/workdesktop/docs/skill-audit/data/sweep-report.json'
d = json.load(open(p, encoding='utf-8'))
st = Counter(r['overall'] for r in d['rows'])
print('gen=', d['generated_at'], dict(st))
print('GREEN=', [r['local_skill'] for r in d['rows'] if r['overall']=='GREEN'])
reg_p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(reg_p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill']=='security-audit':
        e['notes'] = (e.get('notes') or '') + '；UNVERIFIED 成因=代表上游 github/awesome-copilot 无 release tag（版本腿结构性失明，非能力问题）；漂移腿已 GREEN（独有 16→0，字节比 0.996→1.628）'
    e['last_checked'] = '2026-10-20'
json.dump(reg, open(reg_p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('security-audit UNVERIFIED cause recorded')