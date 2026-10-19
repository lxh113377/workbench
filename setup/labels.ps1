# labels.ps1 -- 批量创建/对齐仓库标签（幂等：已存在则更新颜色与描述）
#
# 标签命名原则（清单见 .github/labels.json，该文件必须是**纯 JSON**，
# 不能写注释 —— 实测带 // 注释会让 ConvertFrom-Json 直接失败）：
#   一个标签只表达一个维度，维度间正交，便于组合筛选。
#   bug/enhancement = 类型 | flaky/platform-coupling/reliability/security/
#   performance/docs/test-coverage = 性质 | needs-repro/external-constraint = 处置状态
# 用法: powershell -NoProfile -File setup\labels.ps1
# 前置: gh auth status 已登录且对目标仓库有 admin:org/repo 写权限
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

# 解析 gh 可执行文件（本机实测坑）：PATH 上 'gh' 优先命中无扩展名文件
# C:\Users\<user>\.local\bin\gh，PowerShell 在管道中会拒绝执行并报
# "Cannot run a document in the middle of a pipeline"；真身是
# C:\Program Files\GitHub CLI\gh.exe。故取 Command 中**带 .exe 扩展名**的那个。
$gh = $null
foreach ($c in (Get-Command gh* -CommandType Application -ErrorAction SilentlyContinue)) {
  if ($c.Source -and $c.Source.EndsWith(".exe", [StringComparison]::OrdinalIgnoreCase)) { $gh = $c.Source; break }
}
if (-not $gh) { $gh = "gh" }
Write-Host ("使用 gh: " + $gh)

$json = Get-Content (Join-Path $root ".github\labels.json") -Raw
$existing = & $gh label list --json name --jq ".[].name" 2>$null
foreach ($l in ($json | ConvertFrom-Json)) {
  $have = $false
  foreach ($e in $existing) { if ($e -eq $l.name) { $have = $true; break } }
  if ($have) {
    & $gh label edit $l.name --color $l.color --description $l.description
    Write-Host "updated: $($l.name)"
  } else {
    & $gh label create $l.name --color $l.color --description $l.description
    Write-Host "created: $($l.name)"
  }
}
Write-Host "标签同步完成"
