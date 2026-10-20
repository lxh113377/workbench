import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json',encoding='utf-8'))
names=set(i['name'] for i in d['items'])
for n in ['slides','gstack','ppt-master','screenshot','agent-browser','html-ppt','docx']:
    print(n, 'in inventory:', n in names)
print('total items', len(d['items']))