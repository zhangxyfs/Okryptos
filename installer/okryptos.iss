; Okryptos 安装程序脚本（Inno Setup 6/7）
; 构建：bash scripts/build-installer.sh（先构建 dist/ 再调用 ISCC）

#define AppName "Okryptos"
#define AppVersion "2.26.4"
#define AppPublisher "Okryptos"

[Setup]
AppId={{9F4C3A2E-7B1D-4A5F-9E2C-6D8B1A3F5E70}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={localappdata}\Programs\Okryptos
UsePreviousAppDir=no
DefaultGroupName=Okryptos
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir=output
OutputBaseFilename=OkryptosSetup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
; 升级时语言跟随上次安装（UsePreviousLanguage 默认开，显式声明兜底）；首装才弹语言选择
ShowLanguageDialog=auto
UsePreviousLanguage=yes
; 占用文件的应用程序自动强制关闭不询问（弹"自动关闭?"页实测是升级卡点）；
; RestartApplications 默认开，安装后由 RM 自动拉起
CloseApplications=force
UninstallDisplayName={#AppName} 知识库
SetupIconFile=assets\logo.ico
; 数据目录 ~/.okryptos 由程序运行时创建，卸载默认保留（见 [Code]）

[Languages]
Name: "chinesesimplified"; MessagesFile: "lang\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务："
Name: "addpath"; Description: "将安装目录加入用户 PATH（终端可直接使用 ok 命令）"; GroupDescription: "附加任务："; Flags: unchecked

[Files]
Source: "..\dist\ok.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\okd.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\OkManager.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\OkMeter.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\web\*"; DestDir: "{app}\web"; Flags: ignoreversion recursesubdirs
Source: "..\dist\changelogs\*"; DestDir: "{app}\changelogs"; Flags: ignoreversion recursesubdirs
Source: "..\dist\runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs
Source: "assets\logo.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Okryptos 知识库"; Filename: "{app}\OkManager.exe"; IconFilename: "{app}\logo.ico"; Comment: "打开 Okryptos 配置中心"
Name: "{group}\卸载 Okryptos"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Okryptos 知识库"; Filename: "{app}\OkManager.exe"; IconFilename: "{app}\logo.ico"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "Okryptos"; ValueData: """{app}\okd.exe"""; Flags: uninsdeletevalue

[Run]
; 升级收尾：静默覆盖安装后也拉起新 okd（不带 skipifsilent）；okd 启动时自愈删除 .upgrading 熔断
Filename: "{app}\okd.exe"; Flags: nowait runhidden
; 原样恢复安装前在跑的常驻进程（PrepareToInstall 快照）：GUI 一键静默升级后配置中心
; 与 Token 监视器自己回来，不用用户手动重开。不带 postinstall/skipifsilent——静默也执行。
; 交互式快速升级下若管理器原先在跑，此处恢复后 wpFinished 的 ShellExec 会跳过（见
; CurPageChanged 的 WasOkManagerRunning 判断），不会双开
Filename: "{app}\OkManager.exe"; Flags: nowait; Check: WasOkManagerRunning
Filename: "{app}\OkMeter.exe"; Flags: nowait; Check: WasOkMeterRunning
Filename: "{app}\OkManager.exe"; Description: "打开 Okryptos 配置中心（引导页可一键完成 hooks / 技能 / embedding 配置）"; Flags: postinstall skipifsilent; Check: not IsFastUpgrade

[Code]
const
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{9F4C3A2E-7B1D-4A5F-9E2C-6D8B1A3F5E70}_is1';
  EnvKey = 'Environment';

var
  FinishAutoDone: Boolean;
  { 安装前在跑的常驻进程快照（PrepareToInstall 可能因文件占用重试而多次进入，
    只记首次——重试时进程已被上次 taskkill 杀掉，再记会把"在跑"误记成"没在跑"） }
  ProcessesRecorded: Boolean;
  OkManagerWasRunning: Boolean;
  OkMeterWasRunning: Boolean;

{ [Run] Check 只认无参函数，不认全局变量——包一层 }
function WasOkManagerRunning: Boolean;
begin
  Result := OkManagerWasRunning;
end;

function WasOkMeterRunning: Boolean;
begin
  Result := OkMeterWasRunning;
end;

{ 点分版本号取第 Idx 段（缺段当 0） }
function VersionPart(const S: string; Idx: Integer): Integer;
var
  I, P: Integer;
  R: string;
begin
  R := S;
  for I := 1 to Idx - 1 do
  begin
    P := Pos('.', R);
    if P = 0 then begin Result := 0; exit; end;
    R := Copy(R, P + 1, Length(R));
  end;
  P := Pos('.', R);
  if P > 0 then R := Copy(R, 1, P - 1);
  Result := StrToIntDef(R, 0);
end;

{ 版本比较：A >= B（逐段数值比） }
function VersionGe(const A, B: string): Boolean;
var
  I, Va, Vb: Integer;
begin
  for I := 1 to 4 do
  begin
    Va := VersionPart(A, I);
    Vb := VersionPart(B, I);
    if Va <> Vb then begin Result := Va > Vb; exit; end;
  end;
  Result := True;
end;

function HasPrevInstall: Boolean;
var
  PrevVer: string;
begin
  Result := RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', PrevVer);
end;

{ 快速升级判定：存在旧安装且安装版本 >= 现有版本（含同版重装；降级走完整交互流程） }
function IsFastUpgrade: Boolean;
var
  PrevVer: string;
begin
  Result := RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', PrevVer) and
            VersionGe('{#AppVersion}', PrevVer);
end;

{ 有旧安装时预填旧目录（UsePreviousAppDir=no 下由代码接管默认值） }
procedure InitializeWizard;
var
  PrevDir: string;
begin
  if RegQueryStringValue(HKCU, UninstallKey, 'InstallLocation', PrevDir) and (PrevDir <> '') then
    WizardForm.DirEdit.Text := PrevDir;
end;

{ 快速升级：跳过欢迎/目录/程序组/附加任务/就绪页，双击安装包直接进安装，全程零点击。
  附加任务沿用上次勾选（UsePreviousTasks 默认开）；目录由 InitializeWizard 预填旧目录。
  首次安装与降级（旧版 > 新版）保持完整交互流程 }
function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := IsFastUpgrade and
            ((PageID = wpWelcome) or (PageID = wpSelectDir) or
             (PageID = wpSelectProgramGroup) or (PageID = wpSelectTasks) or
             (PageID = wpReady));
end;

function PathContains(const Path, Dir: string): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') > 0;
end;

procedure AddToUserPath(const Dir: string);
var
  Path: string;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    Path := '';
  if not PathContains(Path, Dir) then
  begin
    if (Path <> '') and (Path[Length(Path)] <> ';') then
      Path := Path + ';';
    Path := Path + Dir;
    RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
  end;
end;

procedure RemoveFromUserPath(const Dir: string);
var
  Path, UpperDir, UpperPath: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    exit;
  UpperDir := Uppercase(Dir);
  UpperPath := Uppercase(Path);
  P := Pos(';' + UpperDir + ';', ';' + UpperPath + ';');
  while P > 0 do
  begin
    Delete(Path, P, Length(Dir) + 1);
    if (P <= Length(Path)) and (Path[P] = ';') then
      Delete(Path, P, 1);
    UpperPath := Uppercase(Path);
    P := Pos(';' + UpperDir + ';', ';' + UpperPath + ';');
  end;
  { Dir 原本位于 PATH 末尾时，删除后残留尾部空条目（尾 ';'），收尾剥掉 }
  while (Length(Path) > 0) and (Path[Length(Path)] = ';') do
    Delete(Path, Length(Path), 1);
  RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
end;

{ 进程是否在跑：tasklist 过滤后 findstr 找镜像名（不依赖 tasklist 的本地化提示文案） }
function IsProcessRunning(const ImageName: string): Boolean;
var
  ResultCode: Integer;
begin
  Result := Exec(ExpandConstant('{cmd}'),
    '/C tasklist /FI "IMAGENAME eq ' + ImageName + '" | findstr /I /C:"' + ImageName + '" >NUL',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
end;

{ 安装前停常驻进程：必须在 wpPreparing 的文件占用检查之前跑，否则 Inno 弹
  "应用程序正在使用文件"页（v2.26.2 实测卡点）。先优雅停 okd，再 taskkill 强杀
  四个进程兜底（OkMeter/OkManager/ok.exe 无 stop 命令）——不问用户；
  最后按路径清安装目录下的孤儿 llama-server sidecar（apply 自退会留孤儿）。
  强杀前快照哪些在跑，装完由 [Run] 段按快照原样恢复（静默升级同样恢复） }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Result := '';
  if not HasPrevInstall then
    exit;
  if not ProcessesRecorded then
  begin
    OkManagerWasRunning := IsProcessRunning('OkManager.exe');
    OkMeterWasRunning := IsProcessRunning('OkMeter.exe');
    ProcessesRecorded := True;
  end;
  if FileExists(ExpandConstant('{app}\okd.exe')) then
    Exec(ExpandConstant('{app}\okd.exe'), 'stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode)
  else if FileExists(ExpandConstant('{app}\ok.exe')) then
    Exec(ExpandConstant('{app}\ok.exe'), 'daemon stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Exec(ExpandConstant('{cmd}'), '/C taskkill /F /IM ok.exe /IM okd.exe /IM OkManager.exe /IM OkMeter.exe >NUL 2>&1',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  { 内置 sidecar llama-server 正常随 okd 优雅停止回收；但 apply 自退（os.Exit 跳过
    defer）/崩溃会留孤儿锁 runtime/ 文件——RM 优雅关它对控制台进程要等满超时
    （实测 ~56s，静默档下甚至可能中止安装）。按路径过滤只杀安装目录下的实例，
    不误伤用户自己运行的 llama.cpp }
  Exec(ExpandConstant('{cmd}'), '/C powershell -NoProfile -Command "Get-Process llama-server -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "' +
       RemoveBackslashUnlessRoot(ExpandConstant('{app}')) + '\*" } | Stop-Process -Force" >NUL 2>&1',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    { 2.25.0 改名（OpenKnowledge→Okryptos）：新 Run 值名 Okryptos 已由 [Registry]
      段写入；旧值名不删会双自启，且卸载后残留指向已删 okd.exe 的死项
      （uninsdeletevalue 只认新名） }
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'OpenKnowledge');
    if WizardIsTaskSelected('addpath') then
      AddToUserPath(ExpandConstant('{app}'));
  end;
end;

{ 脚本层无 TButton.Click 方法，向按钮句柄 PostMessage BM_CLICK 异步触发按钮 }
const
  BM_CLICK = $00F5;

function PostMessageW(hWnd, Msg, wParam, lParam: LongInt): Boolean;
  external 'PostMessageW@user32.dll stdcall';

procedure CurPageChanged(PageID: Integer);
var
  ResultCode: Integer;
begin
  if IsFastUpgrade and (PageID = wpReady) then
  begin
    { Inno 7 的 ShouldSkipPage 对 wpReady 不生效（欢迎/目录/程序组/任务页均已跳过，
      就绪页仍显示）——改为异步点击"安装"自动开始 }
    PostMessageW(WizardForm.NextButton.Handle, BM_CLICK, 0, 0);
    exit;
  end;
  { 快速升级收尾：完成页一出现即自动打开配置中心并自动收向导——安装全程零点击。
    安装段的 postinstall OkManager 项已被 Check: not IsFastUpgrade 排除，不会双开；
    管理器若安装前就在跑，[Run] 恢复项已拉起，这里同样跳过不再开第二个 }
  if (PageID = wpFinished) and IsFastUpgrade and (not FinishAutoDone) then
  begin
    FinishAutoDone := True;
    if not OkManagerWasRunning then
      ShellExec('', ExpandConstant('{app}\OkManager.exe'), '', '', SW_SHOW, ewNoWait, ResultCode);
    PostMessageW(WizardForm.NextButton.Handle, BM_CLICK, 0, 0);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: string;
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    { 文件删除前停常驻 daemon（不存在则 okd.exe 立即返回 0，无害） }
    Exec(ExpandConstant('{app}\okd.exe'), 'stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromUserPath(ExpandConstant('{app}'));
    { 数据目录定位用 %USERPROFILE%：旧写法 userdocs\.. 在 Documents 重定向（OneDrive 已知文件夹
      迁移等）下指错目录，可能找不到真实数据目录甚至误删无关数据 }
    DataDir := ExpandConstant('{%USERPROFILE}\.okryptos');
    { 静默卸载（/VERYSILENT）下绝不删除数据；交互模式才询问。
      注意：卸载期只能用 UninstallSilent，WizardSilent 是 Setup 期函数，误用会运行时错误。 }
    if (not UninstallSilent) and DirExists(DataDir) then
    begin
      { 默认按钮 = 否（MB_DEFBUTTON2）：连续回车不应删全量数据 }
      if MsgBox('是否同时删除知识库数据？' + #13#10 + #13#10 +
                DataDir + #13#10 +
                '（包含全部知识条目、索引与配置。选"否"保留，重装后可继续使用。）',
                mbConfirmation, MB_YESNO + MB_DEFBUTTON2) = IDYES then
        DelTree(DataDir, True, True, True);
    end;
  end;
end;
