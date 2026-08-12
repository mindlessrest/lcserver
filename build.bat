@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

echo === lcserver agent build ===

set ASM=%USERPROFILE%\.gradle\caches\modules-2\files-2.1\org.ow2.asm\asm\9.10.1\ada2141c0cc52ee8f5c48cd5fa4ce0e794f22236\asm-9.10.1.jar

if not exist "%ASM%" (
    echo ASM jar not found, downloading...
    mkdir lib 2>nul
    set ASM=lib\asm-9.7.1.jar
    powershell -Command "Invoke-WebRequest -Uri 'https://repo1.maven.org/maven2/org/ow2/asm/asm/9.7.1/asm-9.7.1.jar' -OutFile '%ASM%'"
    if errorlevel 1 (
        echo Failed to download ASM. Place asm-9.x.jar in lib\ and rerun.
        exit /b 1
    )
)

echo Compiling with %ASM%...

set OUT=build\classes
rmdir /s /q "%OUT%" 2>nul
mkdir "%OUT%"

javac -cp "%ASM%" -d "%OUT%" agent\RedirectAgent.java
if errorlevel 1 (
    echo Compile failed!
    exit /b 1
)

echo Creating JAR...

jar cfm lcserver-agent.jar agent\MANIFEST.MF -C "%OUT%" .
if errorlevel 1 (
    echo JAR creation failed!
    exit /b 1
)

echo.
echo Done: lcserver-agent.jar
echo.
echo Redirect target: wss://api.mindless.rest/ws
echo Usage: -javaagent:lcserver-agent.jar
echo.
echo Place in .minecraft\lunar\ and add to JVM args.
