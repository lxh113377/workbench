import difflib
up=open(r'D:/workdesktop/docs/skill-audit/data/_upstream_dog.md',encoding='utf-8').read().splitlines()
lo=open(r'D:/global_skills/dogfood/SKILL.md',encoding='utf-8').read().splitlines()
sm=difflib.SequenceMatcher(None, lo, up)
n=0
for tag, i1, i2, j1, j2 in sm.get_opcodes():
    if tag=='equal': continue
    n+=1
    print('### %s local[%d:%d] upstream[%d:%d]' % (tag,i1,i2,j1,j2))
    for l in lo[i1:i2][:14]: print('   - ' + l[:130])
    for l in up[j1:j2][:14]: print('   + ' + l[:130])
print('hunks', n)