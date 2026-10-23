@echo off
rem 前端验收一键自检（双击即可，零提示零点击）
cd /d "%~dp0"
node "tests\ui\frontend-gate.mjs" --md "docs\前端门禁回执-latest.md"
echo.
echo 回执: docs\前端门禁回执-latest.md
pause
