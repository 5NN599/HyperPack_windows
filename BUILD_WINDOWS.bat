@echo off
setlocal
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

echo [1/2] Building HyperPack.exe...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o HyperPack.exe .\src\hyperpack_windows.go
if errorlevel 1 goto :error

echo [2/2] Building HyperPack_debug.exe...
go build -trimpath -o HyperPack_debug.exe .\src\hyperpack_windows.go
if errorlevel 1 goto :error

echo.
echo Build complete.
exit /b 0

:error
echo.
echo Build failed. Make sure Go is installed and available on PATH.
exit /b 1
