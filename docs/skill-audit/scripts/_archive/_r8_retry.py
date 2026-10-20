import json, subprocess
P = r'D:/workdesktop/docs/skill-audit/data/_mini-reg.json'
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
sub = {'schema':'skill-watchlist-v1','fuse_days':100,'active_days':400,'watchlist':[e for e in reg['watchlist'] if e['local_skill']=='first-principles-decomposer']}
json.dump(sub, open(P,'w',encoding='utf-8'), ensure_ascii=False, indent=1)
for i in range(3):
    r = subprocess.run(['python', r'D:/workdesktop/docs/skill-audit/scripts/quarterly_skill_sweep.py', '--registry', P, '--json', r'D:/workdesktop/docs/skill-audit/data/_mini-out.json'], capture_output=True)
    d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/_mini-out.json', encoding='utf-8'))
    row = d['rows'][0]
    print('try%d: overall=%s drift=%s %s' % (i+1, row['overall'], row['leg_drift']['state'], row['leg_drift']['note'][:70]))
    if row['overall'] != 'UNVERIFIED': break