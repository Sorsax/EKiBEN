@echo off
setlocal

set SERVICE_NAME=EkibenAgent

rem Stop service if it exists (ignore errors)
sc.exe stop %SERVICE_NAME% >nul 2>&1

rem Wait briefly for stop
ping 127.0.0.1 -n 3 >nul

rem Start service (works even if it was stopped)
sc.exe start %SERVICE_NAME% >nul 2>&1

sc.exe query %SERVICE_NAME%

endlocal
