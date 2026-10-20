import json
from collections import Counter
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
print('gen=', d['generated_at'], dict(Counter(r['overall'] for r in d['rows'])))
print('executing-plans present:', any(r['local_skill']=='executing-plans' for r in d['rows']))
for n in ('subagent-driven-development','story-cover','story-long-write','hyperframes','mcp-builder','A-prompt-better','workbuddy-mcp'):
    r = next((x for x in d['rows'] if x['local_skill']==n), None)
    if r: print('%-28s %-11s ver=%-11s %s' % (n, r['overall'], r['leg_version']['state'], r['leg_version']['note'][:44]))