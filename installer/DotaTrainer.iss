; Inno Setup script for Dota Trainer. Built by installer/build.sh, which passes
; /DAppVersion=<VERSION> and defines the "dotatrainer" sign tool. The release workflow
; passes /DUnsigned instead and signs the finished files separately.

#define AppName "Dota Trainer"
#define AppExe "Dota Trainer.exe"
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6F4C2B1E-8D2A-4C5B-9E3F-1A7D2C9B4E51}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Dota Trainer
AppComments=Live Dota 2 coach
DefaultDirName={localappdata}\Programs\{#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir=.
OutputBaseFilename=DotaTrainer-Setup-{#AppVersion}
SetupIconFile=icon.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
WizardStyle=modern
WizardImageFile=wizard.bmp,wizard-200.bmp
WizardSmallImageFile=wizard-small.bmp,wizard-small-200.bmp
Compression=lzma2/max
SolidCompression=yes
#ifndef Unsigned
SignTool=dotatrainer
SignedUninstaller=yes
#endif
CloseApplications=force
RestartApplications=no
VersionInfoVersion={#AppVersion}
VersionInfoCompany=Dota Trainer
VersionInfoDescription=Dota Trainer Setup
VersionInfoProductName={#AppName}

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"
Name: "startup"; Description: "Start Dota Trainer when I sign in to &Windows"; GroupDescription: "Startup:"

[Files]
Source: "{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.txt"; DestDir: "{app}"; Flags: ignoreversion

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

[Run]
Filename: "{app}\{#AppExe}"; Parameters: "setup"; StatusMsg: "Connecting Dota 2..."; Flags: runhidden waituntilterminated
Filename: "{app}\{#AppExe}"; Description: "Launch Dota Trainer"; Flags: nowait postinstall

[UninstallRun]
Filename: "{app}\{#AppExe}"; Parameters: "quit"; RunOnceId: "QuitApp"; Flags: runhidden waituntilterminated
Filename: "{app}\{#AppExe}"; Parameters: "uninstall -quiet"; RunOnceId: "RemoveGameConfig"; Flags: runhidden waituntilterminated

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
  Existing: String;
begin
  { Ask a running copy to quit cleanly so its exe can be replaced. }
  Existing := ExpandConstant('{app}\{#AppExe}');
  if FileExists(Existing) then
    Exec(Existing, 'quit', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent then
    if MsgBox('Also delete your statistics, recordings and settings?' + #13#10 + #13#10 +
              'Choose No to keep them for a future install.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
      DelTree(ExpandConstant('{app}'), True, True, True);
end;
