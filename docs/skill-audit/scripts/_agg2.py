import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json',encoding='utf-8'))
its=[i for i in d['items'] if i.get('has_skill_md')]
for i in its:
    if i.get('dead_refs'): print('%s: dead=%s ph=%s' % (i['name'], i['dead_refs'], i.get('placeholder_refs')))