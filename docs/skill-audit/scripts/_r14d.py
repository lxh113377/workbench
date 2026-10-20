import json, os, glob
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
items = inv['items']
print('items:', len(items))
phantom = [i['name'] for i in items if i.get('has_skill_md') and not os.path.isfile('D:/global_skills/' + i['name'] + '/SKILL.md')]
print('phantom (has_md but no file):', phantom)
disk = set(os.path.basename(os.path.dirname(p)) for p in glob.glob('D:/global_skills/*/SKILL.md'))
invn = set(i['name'] for i in items if i.get('has_skill_md'))
print('disk=%d inv_with_md=%d' % (len(disk), len(invn)))
print('disk not in inv:', sorted(disk - invn))
print('inv not on disk:', sorted(invn - disk))
mm = [i['name'] for i in items if i.get('name_mismatch')]
print('name_mismatch entries:', mm)