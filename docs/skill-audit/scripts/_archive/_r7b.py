import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'executing-plans':
        print(json.dumps(e, ensure_ascii=False, indent=1))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
r = next((x for x in d['rows'] if x['local_skill']=='executing-plans'), None)
print('sweep row:', json.dumps(r, ensure_ascii=False)[:400] if r else None)
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
s = next((x for x in m['skills'] if x['local_skill']=='executing-plans'), None)
print('matrix has it:', s is not None)