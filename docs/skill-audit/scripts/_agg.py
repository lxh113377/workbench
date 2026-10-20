import json
d=json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json',encoding='utf-8'))
its=[i for i in d['items'] if i.get('has_skill_md')]
tot=sum(i.get('route_top1_count') or 0 for i in its)
srt=sorted(its,key=lambda x:-(x.get('route_top1_count') or 0))
top10=sum(i.get('route_top1_count') or 0 for i in srt[:10])
never=[i['name'] for i in its if not (i.get('route_top1_count') or 0)]
print('total_top1=%d top10_share=%.1f%% never_used=%d' % (tot, 100.0*top10/max(tot,1), len(never)))
print('dead_refs_detail:')
for i in its:
    if i.get('dead_refs'): print('  %s: %s' % (i['name'], i['dead_refs']))
print('no_version_count=%d' % len([i for i in its if not i.get('version')]))
print('no_version_sample=%s' % ', '.join(i['name'] for i in its if not i.get('version'))[:400])
big=[i for i in its if i['skill_md_bytes']>12288]
print('over12k=%d sums=%d bytes' % (len(big), sum(i['skill_md_bytes'] for i in big)))
print('with_refs=%d' % len([i for i in its if i['ref_files']>0]))
print('dir_bytes_top10=%s' % ', '.join('%s:%dMB' % (i['name'], i['dir_bytes']//1048576) for i in sorted(its,key=lambda x:-x['dir_bytes'])[:10]))