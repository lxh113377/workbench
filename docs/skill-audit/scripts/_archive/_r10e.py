import json
p = r'D:/workdesktop/docs/skill-audit/data/score-matrix.json'
m = json.load(open(p, encoding='utf-8'))
n = 0
for s in m['skills']:
    for c in s.get('candidates', []):
        if c.get('repo') == 'skills-lock/skil-lock' and c.get('stars') is None:
            c['stars_note'] = 'ungh 403 + api 配额尽（2026-10-20 16:40 实测），双通道限流期间取不到；S 维记 null 不猜分，下季窗口重测'
            n += 1
json.dump(m, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('annotated:', n)
miss = [(s['local_skill'], c.get('repo')) for s in m['skills'] for c in s.get('candidates', []) if c.get('stars') is None]
print('still null (annotated):', miss)
print('total candidates:', sum(s.get('candidate_count',0) for s in m['skills']))