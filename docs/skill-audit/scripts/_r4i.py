import re
def norm(h):
    h = re.sub(r'^#+\s*', '', h).lower()
    h = re.sub(r'[（(][^）)]*[）)]\s*$', '', h).strip()
    h = re.sub(r'[：:]\s*$', '', h).strip()
    h = re.sub(r'^(skill|skill:)\s*', '', h)
    h = re.sub(r'[\s:\-–—]+', ' ', h).strip()
    return h
cases = [
  '## Discover the Stack First / Discover the Stack First（先探栈，再写第一个测试）',
  '## Discover the Stack First（先探栈，再写第一个测试）/ Discover the Stack First',
  '## Discover the Stack First（先探栈，再写第一个测试）',
]
for c in cases: print(repr(norm(c)), '<-', c[:60])
print()
print('upstream target: discover the stack first')