import json, os
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
names_inv = set(i['name'] for i in inv['items'] if i.get('has_skill_md'))
names_reg = set(e['local_skill'] for e in reg['watchlist'])
print('inv=%d reg=%d' % (len(names_inv), len(names_reg)))
print('inv not in reg:', sorted(names_inv - names_reg))
# 183 vs 上次 182：找新增者（对比 git 未跟踪目录）
new = sorted(names_inv - names_reg)
for n in new:
    p = 'D:/global_skills/' + n + '/SKILL.md'
    print('  ', n, '| bytes:', os.path.getsize(p) if os.path.isfile(p) else 'N/A')
    if os.path.isfile(p):
        t = open(p, encoding='utf-8', errors='replace').read(400)
        print('     head:', t[:200].replace(chr(10), ' / '))