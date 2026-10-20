import os, glob
for n in ('test-driven-development','first-principles-decomposer','A-dynamic-workflow','frontend-slides'):
    d = 'D:/global_skills/' + n
    main = os.path.join(d, 'SKILL.md')
    mb = os.path.getsize(main) if os.path.isfile(main) else 0
    refs = glob.glob(d + '/references/*.md')
    rb = sum(os.path.getsize(f) for f in refs)
    print('%-30s main=%6d refs=%3d files total=%6d' % (n, mb, len(refs), mb+rb))
    for f in refs[:6]: print('      ', os.path.basename(f), os.path.getsize(f))