import json
inv = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
items = inv['items']
print('item keys:', sorted(items[0].keys()))
print()
for i in items[:3]:
    print(json.dumps(i, ensure_ascii=False)[:400])
print()
print('top-level keys:', sorted(inv.keys()))
print('summary:', json.dumps({k:v for k,v in inv.items() if k != 'items'}, ensure_ascii=False)[:600])