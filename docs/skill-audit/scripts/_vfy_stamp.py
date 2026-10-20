import re
for n in ['algorithmic-art','writing-plans','mcp-builder','天眼一下','story']:
    raw = open(r'D:/global_skills/%s/SKILL.md' % n, 'rb').read()
    bom = raw[:3] == b'\xef\xbf\xbf'
    t = raw.decode('utf-8-sig', errors='replace')
    lines = t.splitlines()
    end = lines.index('---', 1)
    ver = [l for l in lines[1:end] if l.startswith('version:')]
    trg = sum(1 for l in lines[1:end] if l.strip().startswith('- '))
    print('%-18s bom=%s fm_lines=%d ver=%s triggers=%d crlf=%s' % (n, bom, end, ver, trg, b'\r\n' in raw[:500]))