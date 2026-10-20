import json
p=r'D:/workdesktop/docs/skill-audit/data/_vbc-check.json'
import os
if os.path.isfile(p):
    d=json.load(open(p,encoding='utf-8'))
    r=[x for x in d['rows'] if x['local_skill']=='verification-before-completion'][0]
    print('overall=',r['overall'])
    print('drift:',r['leg_drift']['state'], r['leg_drift']['note'])
else:
    print('no file')