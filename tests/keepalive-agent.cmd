@echo off
rem Keepalive wrapper for the experiment root D:\workbench-agent.
rem Why: tests\keepalive.ps1 defaults $Exe to the dist root, so the agent-root engine had no
rem supervisor after it died (12h of zero task activity, observed 2026-10-28). schtasks /TR has a
rem 261-char limit and the full powershell command line with three params exceeds it when launched
rem via conhost, hence this .cmd. Keep this file ASCII-only: cmd.exe reads it in the OEM codepage,
rem and non-ASCII rem lines get mangled and executed as commands (observed 2026-10-28).
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File D:\workdesktop\tests\keepalive.ps1 -Exe D:\workbench-agent\workbench.exe -RuntimeDir D:\workbench-agent\runtime -DataRoot D:\workbench-agent\workbench-output
