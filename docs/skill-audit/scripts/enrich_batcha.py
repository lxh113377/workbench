import json
BASE = r'D:/workdesktop/docs/skill-audit/data/'
a = json.load(open(BASE + 'score-matrix-batchA.json', encoding='utf-8'))
facts = {r['repo']: r for r in json.load(open(BASE + 'gh-repo-facts-v2.json', encoding='utf-8'))['repos']}
extra = {'A-dynamic-workflow': [('wshobson/agents','plugins/agent-teams/skills/task-coordination-strategies/SKILL.md',4690)],
         'ppt-master': [('wshobson/agents','plugins/pptx-deck-creation/skills/pptx-quality-gates/SKILL.md',1635),
                        ('Akxan/ppt-agent-skill','SKILL.md',21090)]}
print('facts keys', list(facts)[:6])
for s in a['skills']:
    for c in s['candidates']:
        f = facts.get(c['repo'])
        if f:
            if c.get('stars') is None: c['stars'] = f.get('stars')
            if not c.get('last_activity'): c['last_activity'] = (str(f.get('pushed_at'))[:10] if f.get('pushed_at') else None)
print('enriched')
json.dump(a, open(BASE + 'score-matrix-batchA.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)