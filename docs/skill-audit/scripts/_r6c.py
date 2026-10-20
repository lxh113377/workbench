import json
p = r'D:/workdesktop/docs/skill-audit/data/score-matrix.json'
m = json.load(open(p, encoding='utf-8'))
n = 0
for s in m['skills']:
    for c in s.get('candidates', []):
        # 套件仓星值口径：入口件已定位(conf=high)的，星值记「仓级」并注明；未定位的(conf=low)星值不计入 S 的可信度
        if c.get('stars') and (c.get('stars') or 0) >= 10000 and not c.get('evidence'):
            c['stars_scope'] = 'repo_level_but_entry_unlocated(套件仓星值属整仓，本技能入口件未定位 ⇒ S 分仅供参考，不得作为替换依据)'
            c['confidence'] = 'low'
            n += 1
        elif c.get('stars') and (c.get('stars') or 0) >= 10000 and c.get('evidence'):
            c['stars_scope'] = 'repo_level(套件仓星值属整仓；入口件已实测定位，社区支持度按仓级读)'
            n += 1
json.dump(m, open(p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('stars_scope annotated:', n)
# 重新生成 03 文档
import subprocess
r = subprocess.run(['python', r'D:/workdesktop/docs/skill-audit/scripts/score_matrix.py'], capture_output=True)
print('regen rc', r.returncode, (r.stdout + r.stderr).decode('utf-8','replace')[-200:])