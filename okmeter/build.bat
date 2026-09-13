@echo off
chcp 65001 >nul
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

rem -- source groups: tests must not include render/ or ui/app.cpp ui/watch.cpp --
set "CORE=core\*.cpp"
set "ADAPT=adapters\kimi\*.cpp"
set "UI_CORE=ui\geometry.cpp"
set "UI_WIN=ui\app.cpp ui\menu.cpp ui\settings.cpp ui\watch.cpp"
set "RENDER=render\*.cpp render\forms\*.cpp render\materials\*.cpp"
set "SYSLIBS=d3d11.lib d2d1.lib dwrite.lib dcomp.lib dxgi.lib windowscodecs.lib user32.lib gdi32.lib windowsapp.lib wtsapi32.lib"

echo [1/3] 编译单测...
cl %FLAGS% tests\*.cpp %CORE% %ADAPT% %UI_CORE% /Fo:build\ /Fe:build\okmeter-tests.exe || (popd & exit /b 1)

echo [2/3] 运行单测...
build\okmeter-tests.exe || (popd & exit /b 1)

echo [3/3] 编译 OkMeter.exe...
rc /nologo /fo build\version.res version.rc || (popd & exit /b 1)
cl %FLAGS% app\main.cpp %CORE% %ADAPT% %UI_CORE% %UI_WIN% %RENDER% build\version.res %SYSLIBS% /Fo:build\ /Fe:build\OkMeter.exe /link /SUBSYSTEM:WINDOWS /ENTRY:mainCRTStartup || (popd & exit /b 1)

echo 完成：build\OkMeter.exe
popd
