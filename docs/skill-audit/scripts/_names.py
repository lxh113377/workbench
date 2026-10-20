import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/gh-trees.json',encoding='utf-8'))
for t in d['trees']:
    items=t.get('items') or []
    print('### %s/%s  n=%d' % (t['repo'], t['sub'], len(items)))
    print('   ' + ', '.join(items))