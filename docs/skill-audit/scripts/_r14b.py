import json, os, subprocess, datetime
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
print('inventory generated_at:', inv.get('generated_at'))
items = [i for i in inv['items'] if i.get('has_skill_md')]
# 按 mtime 排序找最新的
rows = []
for i in items:
    p = 'D:/global_skills/' + i['name'] + '/SKILL.md'
    if os.path.isfile(p):
        rows.append((datetime.datetime.fromtimestamp(os.path.getsize(p) and os.path.getmtime(p)), i['name'], os.path.getsize(p)))
rows.sort(reverse=True)
print('=== 最新 8 个 SKILL.md ===')
for ts, n, sz in rows[:8]:
    print('  %s  %-40s %6d B' % (ts.strftime('%m-%d %H:%M'), n, sz))
print('total:', len(rows))