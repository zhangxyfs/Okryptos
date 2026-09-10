@echo off
setlocal EnableExtensions
pushd "%~dp0"

rem ── 定位 MSVC（vswhere → vcvars64）──
set "VSW=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSW%" set "VSW=%ProgramFiles%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSW%" (echo 错误：未找到 vswhere，请安装 VS Build Tools & popd & exit /b 1)
set "VS="
for /f "usebackq delims=" %%i in (`"%VSW%" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "VS=%%i"
if not defined VS (echo 错误：未找到 MSVC C++ 工具集 & popd & exit /b 1)
call "%VS%\VC\Auxiliary\Build\vcvars64.bat" >nul || (echo 错误：vcvars64 初始化失败 & popd & exit /b 1)

if not exist build mkdir build
set "FLAGS=/nologo /std:c++20 /EHsc /W4 /utf-8 /O2 /MT /I."

echo [1/3] 编译单测...
cl %FLAGS% tests\*.cpp core\*.cpp adapters\kimi\*.cpp /Fo:build\ /Fe:build\okmeter-tests.exe || (popd & exit /b 1)

echo [2/3] 运行单测...
build\okmeter-tests.exe || (popd & exit /b 1)

echo [3/3] 编译 OkMeter.exe...
rc /nologo /fo build\version.res version.rc || (popd & exit /b 1)
cl %FLAGS% app\main.cpp core\*.cpp adapters\kimi\*.cpp build\version.res /Fo:build\ /Fe:build\OkMeter.exe || (popd & exit /b 1)

echo 完成：build\OkMeter.exe
popd
