import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
old = next((e for e in reg['watchlist'] if e['local_skill'] == 'executing-plans'), None)
assert old, 'executing-plans entry missing'
# 把旧条目的候选并入 sdd（旧条目候选有 4 个，含 addyosmani/incremental-implementation 与 awesome-copilot 两个 sdd 没有的）
sdd = next((e for e in reg['watchlist'] if e['local_skill'] == 'subagent-driven-development'), None)
assert sdd, 'sdd entry missing'
have = set(c['repo'] + '|' + str(c.get('entry_path')) for c in (sdd.get('candidates') or []))
merged = 0
for c in (old.get('candidates') or []):
    key = c['repo'] + '|' + str(c.get('entry_path'))
    if key not in have:
        sdd.setdefault('candidates', []).append(dict(c, merged_from='executing-plans(轮7 合并，旧名注销前移交接管)'))
        merged += 1
sdd['candidate_count'] = len(sdd['candidates'])
sdd['notes'] = (sdd.get('notes') or '') + ' | 2026-10-20 轮7：注销同名旧条目 executing-plans（其 4 候选中 %d 个未在本条目的已并入；该旧条目 local_skill_md 指向不存在的目录，sweep local_main_bytes=0，职责由本条取代）' % merged
reg['watchlist'] = [e for e in reg['watchlist'] if e['local_skill'] != 'executing-plans']
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('merged candidates:', merged, '| sdd now has', len(sdd['candidates']), '| entries:', len(reg['watchlist']))