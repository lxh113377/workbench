import json
m = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))
print('skills=', len(m['skills']), 'candidates=', sum(s.get('candidate_count', 0) for s in m['skills']))
sdd = [s for s in m['skills'] if s['local_skill']=='subagent-driven-development']
print('sdd in matrix:', len(sdd)==1)
if sdd: print('  best=', sdd[0]['best'], '| local_bytes=', sdd[0]['local_bytes'])