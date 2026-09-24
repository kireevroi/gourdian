; Inno Setup script for Gourdian. Built by installer/build.sh, which passes
; /DAppVersion=<VERSION> and defines the "gourdian" sign tool. The release workflow
; passes /DUnsigned instead and signs the finished files separately.

#ifndef AppName
  #define AppName "Gourdian"
#endif
#define AppExe "Gourdian.exe"
; The app's id for Windows; config.AppID in the Go code must match it. A test build can pass
; its own /DAppGuid and /DAppName to install next to the real app.
#ifndef AppGuid
  #define AppGuid "6F4C2B1E-8D2A-4C5B-9E3F-1A7D2C9B4E51"
#endif
; The app's name before 1.5. The AppId below never changes, so an upgrade from Dota Trainer
; installs over it, into its folder (where the data is), and takes the old files away.
#define LegacyName "Dota Trainer"
#define LegacyExe "Dota Trainer.exe"
#ifndef NumVersion
  #define NumVersion "0.0.0"
#endif
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{{#AppGuid}}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Gourdian
AppComments=Live Dota 2 coach
DefaultDirName={localappdata}\Programs\{#AppName}
; Always ask where to install, upgrades included. The app keeps its data next to itself, so an
; upgrade into another folder moves the data along ([Code]).
DisableDirPage=no
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir=.
OutputBaseFilename=Gourdian-Setup-{#AppVersion}
SetupIconFile=icon.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
WizardStyle=modern
WizardImageFile=wizard.bmp,wizard-200.bmp
WizardSmallImageFile=wizard-small.bmp,wizard-small-200.bmp
Compression=lzma2/max
SolidCompression=yes
#ifndef Unsigned
SignTool=gourdian
SignedUninstaller=yes
#endif
CloseApplications=force
RestartApplications=no
VersionInfoVersion={#NumVersion}
VersionInfoCompany=Gourdian
VersionInfoDescription=Gourdian Setup
VersionInfoProductName={#AppName}

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"
Name: "startup"; Description: "Start Gourdian when I sign in to &Windows"; GroupDescription: "Startup:"

[Files]
Source: "{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion
Source: "THIRD_PARTY_NOTICES.txt"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
Type: files; Name: "{app}\{#LegacyExe}"
Type: files; Name: "{userprograms}\{#LegacyName}.lnk"
Type: files; Name: "{userdesktop}\{#LegacyName}.lnk"

[Dirs]
Name: "{app}\stats"
Name: "{app}\recordings"
Name: "{app}\logs"
Name: "{app}\cache"

[Icons]
Name: "{userprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; WorkingDir: "{app}"; Comment: "Live Dota 2 coach"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; WorkingDir: "{app}"; Comment: "Live Dota 2 coach"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "{#AppName}"; ValueData: """{app}\{#AppExe}"" --background"; Flags: uninsdeletevalue; Tasks: startup
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "{#AppName}"; Flags: deletevalue; Tasks: not startup
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "{#LegacyName}"; Flags: deletevalue

[Run]
Filename: "{app}\{#AppExe}"; Parameters: "setup"; StatusMsg: "Connecting Dota 2..."; Flags: runhidden waituntilterminated
Filename: "{app}\{#AppExe}"; Description: "Launch Gourdian"; Flags: nowait postinstall

[UninstallRun]
Filename: "{app}\{#AppExe}"; Parameters: "quit"; RunOnceId: "QuitApp"; Flags: runhidden waituntilterminated
Filename: "{app}\{#AppExe}"; Parameters: "uninstall -quiet"; RunOnceId: "RemoveGameConfig"; Flags: runhidden waituntilterminated

[Code]
{ What the app keeps in its folder. Upgrades into another folder move these, and uninstalling
  deletes only these when asked: the folder may have been one the player already used. }
function DataFiles(): TArrayOfString;
begin
  Result := ['trainer.data', 'trainer.data-wal', 'trainer.data-shm', 'config.json', 'config.json.bad',
    'config.json.tmp', 'secrets.json', 'secrets.json.tmp', 'rules.json', 'draft-seen'];
end;

function DataDirs(): TArrayOfString;
begin
  Result := ['stats', 'recordings', 'logs', 'cache', 'tools', 'piper'];
end;

procedure QuitRunning(Exe: String);
var
  ResultCode: Integer;
begin
  if FileExists(Exe) then
    Exec(Exe, 'quit', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure QuitRunningIn(Dir: String);
begin
  QuitRunning(AddBackslash(Dir) + '{#AppExe}');
  QuitRunning(AddBackslash(Dir) + '{#LegacyExe}');
end;

function SamePath(A, B: String): Boolean;
begin
  Result := CompareText(RemoveBackslashUnlessRoot(ExpandFileName(A)), RemoveBackslashUnlessRoot(ExpandFileName(B))) = 0;
end;

{ An upgrade into a different folder than the previous install's. }
function MovingFolders(): Boolean;
begin
  Result := (WizardForm.PrevAppDir <> '') and not SamePath(WizardForm.PrevAppDir, WizardDirValue);
end;

{ The app writes its data next to itself, so its folder must be writable without admin rights. }
function CanWriteTo(Dir: String): Boolean;
var
  Existed: Boolean;
  Probe: String;
begin
  Existed := DirExists(Dir);
  Probe := AddBackslash(Dir) + '.gourdian-write-check';
  Result := (Existed or ForceDirectories(Dir)) and SaveStringToFile(Probe, '', False);
  DeleteFile(Probe);
  if not Existed then
    RemoveDir(Dir);
end;

function NotWritableMessage(Dir: String): String;
begin
  Result := 'Gourdian keeps its settings and statistics in its own folder, so it needs a folder you can write to ' +
    'without administrator rights, and it can''t write to ' + Dir + '.' + #13#10#13#10 +
    'Choose another folder, for example on another drive or in your user folder (not Program Files).';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (CurPageID = wpSelectDir) and not CanWriteTo(WizardDirValue) then
  begin
    SuppressibleMsgBox(NotWritableMessage(WizardDirValue), mbError, MB_OK, IDOK);
    Result := False;
  end;
end;

{ Quotes a folder for robocopy, which reads a backslash before a closing quote as an escape. }
function RoboPath(Dir: String): String;
begin
  Dir := RemoveBackslashUnlessRoot(Dir);
  if Copy(Dir, Length(Dir), 1) = '\' then
    Dir := Dir + '.';
  Result := '"' + Dir + '"';
end;

function Robocopy(Params: String): Boolean;
var
  ResultCode: Integer;
begin
  { robocopy moves across drives too; exit codes of 8 and above mean something failed. }
  Result := Exec(ExpandConstant('{sys}\robocopy.exe'), Params + ' /R:2 /W:1 /NP /NFL /NDL /NJH /NJS',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode < 8);
end;

{ Moves the data from the previous install's folder into the new one: the data file, settings,
  API keys, rules, and the stats, recordings, logs, cache, AI tools and voice folders. }
function MoveData(FromDir, ToDir: String): String;
var
  I: Integer;
  Files: String;
  Names, Dirs: TArrayOfString;
begin
  Result := '';
  if (FileExists(AddBackslash(FromDir) + 'trainer.data') and FileExists(AddBackslash(ToDir) + 'trainer.data')) or
     (FileExists(AddBackslash(FromDir) + 'config.json') and FileExists(AddBackslash(ToDir) + 'config.json')) then
  begin
    SuppressibleMsgBox('The new folder already has Gourdian settings and statistics, so Gourdian will use those. ' +
      'The ones in ' + FromDir + ' stay there.', mbInformation, MB_OK, IDOK);
    Exit;
  end;
  Files := '';
  Names := DataFiles();
  for I := 0 to GetArrayLength(Names) - 1 do
    Files := Files + ' ' + Names[I];
  if not Robocopy(RoboPath(FromDir) + ' ' + RoboPath(ToDir) + Files + ' /MOV') then
  begin
    Result := 'Couldn''t move your settings and statistics from ' + FromDir + ' to ' + ToDir + '.';
    Exit;
  end;
  Dirs := DataDirs();
  for I := 0 to GetArrayLength(Dirs) - 1 do
    if DirExists(AddBackslash(FromDir) + Dirs[I]) and
       not Robocopy(RoboPath(AddBackslash(FromDir) + Dirs[I]) + ' ' + RoboPath(AddBackslash(ToDir) + Dirs[I]) + ' /E /MOVE') then
    begin
      Result := 'Couldn''t move ' + AddBackslash(FromDir) + Dirs[I] + ' to ' + ToDir + '.';
      Exit;
    end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  { Ask running copies to quit cleanly so their files can be replaced or moved, under either name. }
  QuitRunningIn(WizardDirValue);
  if WizardForm.PrevAppDir <> '' then
    QuitRunningIn(WizardForm.PrevAppDir);
  if not CanWriteTo(WizardDirValue) then
  begin
    Result := NotWritableMessage(WizardDirValue);
    Exit;
  end;
  if MovingFolders() then
    Result := MoveData(WizardForm.PrevAppDir, WizardDirValue);
end;

{ After an upgrade into another folder, the old folder only holds the old program: remove it. }
procedure RemoveOldApp(Dir: String);
var
  Found: TFindRec;
  I: Integer;
  Dirs: TArrayOfString;
begin
  Dir := AddBackslash(Dir);
  DeleteFile(Dir + '{#AppExe}');
  DeleteFile(Dir + '{#LegacyExe}');
  DeleteFile(Dir + 'README.txt');
  DeleteFile(Dir + 'LICENSE.txt');
  DeleteFile(Dir + 'THIRD_PARTY_NOTICES.txt');
  if FindFirst(Dir + 'unins*', Found) then
  try
    repeat
      DeleteFile(Dir + Found.Name);
    until not FindNext(Found);
  finally
    FindClose(Found);
  end;
  Dirs := DataDirs();
  for I := 0 to GetArrayLength(Dirs) - 1 do
    RemoveDir(Dir + Dirs[I]);
  RemoveDir(RemoveBackslash(Dir));
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and MovingFolders() then
    RemoveOldApp(WizardForm.PrevAppDir);
end;

{ Deletes what the app keeps in its folder, and the folder once nothing else is in it. Never the
  whole folder: it may be one the player chose that holds other things too. }
procedure DeleteData(Dir: String);
var
  I: Integer;
  Names: TArrayOfString;
begin
  Dir := AddBackslash(Dir);
  Names := DataFiles();
  for I := 0 to GetArrayLength(Names) - 1 do
    DeleteFile(Dir + Names[I]);
  Names := DataDirs();
  for I := 0 to GetArrayLength(Names) - 1 do
    DelTree(Dir + Names[I], True, True, True);
  RemoveDir(RemoveBackslash(Dir));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent then
    if MsgBox('Also delete your statistics, recordings and settings?' + #13#10 + #13#10 +
              'Choose No to keep them for a future install.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
      DeleteData(ExpandConstant('{app}'));
end;
