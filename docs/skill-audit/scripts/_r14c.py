import json, os
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
items = inv['items']
print('items:', len(items), '| with_skill_md:', sum(1 for i in items if i.get('has_skill_md')))
# 找 has_skill_md=True 但文件不存在的
for i in items:
    if i.get('has_skill_md'):
        p = 'D:/global_skills/' + i['name'] + '/SKILL.md'
        if not os.path.isfile(p):
            print('  PHANTOM:', i['name'], '| dir:', i.get('dir'), '| bytes field:', i.get('skill_md_bytes'))
# 反向：磁盘有但 inventory 没有
import glob
disk = set(os.path.basename(os.path.dirname(p)) for p in glob.glob('D:/global_skills/*/SKILL.md'))
invn = set(i['name'] for i in items)
print('disk not in inventory:', sorted(disk - invn))
print('counts: disk=%d inv=%d' % (len(disk), len(invn)))