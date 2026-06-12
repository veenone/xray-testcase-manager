; Inno Setup script for Xray Test Manager.
;
; Replaces the previous Wails/NSIS installer. Driven by scripts/release.ps1,
; which compiles it with ISCC and passes the version + build paths as /D
; defines:
;   ISCC /DAppVersion=1.2.2 /DSourceDir=<build\bin> /DOutputDir=<dist> \
;        [/DWebView2Bootstrapper=<path to MicrosoftEdgeWebview2Setup.exe>] installer.iss
;
; The defines have sensible fallbacks so the script also compiles standalone
; (e.g. opening it in the Inno Setup IDE) after a `wails build`.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\bin"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\..\dist"
#endif

#define AppName "Xray Test Manager"
#define AppPublisher "Achmad Fienan Rahardianto"
#define AppExeName "xray-test-manager.exe"
#define AppUrl "https://github.com/veenone/xray-testcase-manager"

[Setup]
; A stable AppId keeps upgrades and uninstall entries consistent across
; versions — do not change it once released.
AppId={{70AFE0FB-5066-4CF2-8F3E-180CA778DB94}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppUrl}
AppSupportURL={#AppUrl}
AppUpdatesURL={#AppUrl}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#AppExeName}
OutputDir={#OutputDir}
OutputBaseFilename=xray-test-manager-{#AppVersion}-windows-amd64-installer
SetupIconFile=..\icon.ico
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
; Per-machine install (Program Files) requires elevation.
PrivilegesRequired=admin

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion
#ifdef WebView2Bootstrapper
Source: "{#WebView2Bootstrapper}"; DestDir: "{tmp}"; DestName: "MicrosoftEdgeWebview2Setup.exe"; Flags: deleteafterinstall
#endif

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{group}\{cm:UninstallProgram,{#AppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
#ifdef WebView2Bootstrapper
; Install the Evergreen WebView2 runtime when it isn't already present (matches
; the old NSIS installer's wails.webview2runtime behaviour).
Filename: "{tmp}\MicrosoftEdgeWebview2Setup.exe"; Parameters: "/silent /install"; StatusMsg: "Installing Microsoft Edge WebView2 runtime…"; Check: WebView2Missing
#endif
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,{#AppName}}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Remove the app's WebView2 user-data folder, as the NSIS uninstaller did.
Type: filesandordirs; Name: "{localappdata}\{#AppExeName}"

[Code]
// WebView2Missing reports whether the Evergreen WebView2 runtime is absent, so
// the bootstrapper only runs when needed. The Evergreen runtime registers a
// version ("pv") under this well-known client GUID, per-machine (HKLM, under
// WOW6432Node on 64-bit) or per-user (HKCU).
function WebView2Missing(): Boolean;
var
  pv: String;
begin
  Result := not (
    RegQueryStringValue(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', pv) or
    RegQueryStringValue(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', pv) or
    RegQueryStringValue(HKCU, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', pv)
  );
end;
