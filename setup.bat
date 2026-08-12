@echo off
echo === lcserver setup ===
echo.

:: Add hosts entries (requires admin)
echo Adding hosts entries...
echo.

set "HOSTS=%SystemRoot%\System32\drivers\etc\hosts"

findstr /C:"lunarclientprod.com" "%HOSTS%" >nul 2>&1
if %errorlevel% neq 0 (
    echo 127.0.0.1 authenticator.lunarclientprod.com>> "%HOSTS%"
    echo 127.0.0.1 websocket.lunarclientprod.com>> "%HOSTS%"
    echo   [+] Added hosts entries
) else (
    echo   [*] Hosts entries already present
)

echo.
echo === Next steps ===
echo 1. Run: lcserver.exe
echo    (auto-generates TLS cert on first run, listens on :443)
echo.
echo 2. Copy agent: copy lcserver-agent.jar ^%USERPROFILE^%\.lunarclient\
echo.
echo 3. Lunar Client launcher -^> Settings -^> JVM Args:
echo    -javaagent:^%USERPROFILE^%\.lunarclient\lcserver-agent.jar
echo.
echo 4. Launch LC
echo.
echo Admin panel: http://localhost:8080
echo.
pause
