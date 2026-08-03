@echo off
setlocal
REM === Stele LAN Port Forwarding ===
REM Run as Administrator whenever the WSL2 address changes.
REM stele must listen on 0.0.0.0:8989 inside WSL2.

net session >nul 2>&1
if errorlevel 1 (
    echo ERROR: Run this script as Administrator.
    pause
    exit /b 1
)

set "WSL_IP="
echo Checking WSL2 IP...
for /f "tokens=1" %%i in ('wsl.exe hostname -I') do if not defined WSL_IP set "WSL_IP=%%i"
if not defined WSL_IP (
    echo ERROR: Could not determine the WSL2 IP address.
    pause
    exit /b 1
)
echo WSL2 IP: %WSL_IP%

echo.
echo Forwarding Windows:8989 to %WSL_IP%:8989...
netsh interface portproxy delete v4tov4 listenport=8989 listenaddress=0.0.0.0 >nul 2>&1
netsh interface portproxy add v4tov4 listenport=8989 listenaddress=0.0.0.0 connectport=8989 connectaddress=%WSL_IP%
if errorlevel 1 (
    echo ERROR: Failed to configure the Windows port proxy.
    pause
    exit /b 1
)

echo.
echo Allowing TCP 8989 from the local subnet...
netsh advfirewall firewall delete rule name="Stele" >nul 2>&1
netsh advfirewall firewall delete rule name="Stele 8989" >nul 2>&1
netsh advfirewall firewall add rule name="Stele 8989" dir=in action=allow protocol=TCP localport=8989 remoteip=LocalSubnet profile=any
if errorlevel 1 (
    echo ERROR: Failed to configure Windows Firewall.
    pause
    exit /b 1
)

echo.
echo === Done ===
echo Share URL base: http://YOUR_WINDOWS_IP:8989
echo Get your Windows IP: ipconfig ^| findstr IPv4
echo.
echo Share links follow the host you opened the panel on, so they track this IP
echo as DHCP moves it. Use --share-url only for a proxy or DNS name the panel
echo cannot see from the inside.
pause
