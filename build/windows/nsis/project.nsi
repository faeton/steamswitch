;SteamSwitch
;Fork of TcNo Account Switcher by TroubleChute (Wesley Pyburn)
;https://github.com/faeton/steamswitch

;--------------------------------
;Defaults (override via -D on makensis command line)

!ifndef VERSION
  !define VERSION "0.0.0.0"
!endif
!ifndef DISPLAY_VERSION
  !define DISPLAY_VERSION "0.0.0_0"
!endif

!ifndef INSTALLER_7Z
  !define INSTALLER_7Z "..\..\..\bin\SteamSwitch.7z"
!endif
!ifndef OPEN_SOURCE_LICENSES_TXT
  !define OPEN_SOURCE_LICENSES_TXT "..\..\..\OPEN_SOURCE_LICENSES.txt"
!endif

;--------------------------------
;Include Modern UI and helpers

  !include "MUI2.nsh"
  !include "nsDialogs.nsh"
  !include "FileFunc.nsh"
  !include "x64.nsh"
  !include "WinVer.nsh"

;--------------------------------
;Variables

!define APP_NAME "SteamSwitch"
!define PRODUCT_EXECUTABLE "SteamSwitch.exe"
!define LNK_NAME "SteamSwitch.lnk"
!define COMP_NAME "Ivan Faeton Danishevskyi"
!define WEB_SITE "https://github.com/faeton/steamswitch"
!define COPYRIGHT "(c) 2026 Ivan Faeton Danishevskyi. Based on TcNo Account Switcher, (c) TroubleChute (Wesley Pyburn)."
!define DESCRIPTION "SteamSwitch Installer"
!define INSTALL_TYPE "SetShellVarContext current"
!define REG_ROOT "HKCU"
!define REG_APP_PATH "Software\Microsoft\Windows\CurrentVersion\App Paths\${PRODUCT_EXECUTABLE}"
!define UNINSTALL_EXE "Uninstall SteamSwitch.exe"
!define UNINSTALL_LNK_NAME "Uninstall SteamSwitch.lnk"
!define UNINST_PATH "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}"

!define REG_START_MENU "Start Menu Folder"
!define SM_Folder "SteamSwitch"

VIProductVersion  "${VERSION}"
VIAddVersionKey "ProductName"  "${APP_NAME}"
VIAddVersionKey "CompanyName"  "${COMP_NAME}"
VIAddVersionKey "LegalCopyright"  "${COPYRIGHT}"
VIAddVersionKey "FileDescription"  "${DESCRIPTION}"
VIAddVersionKey "FileVersion"  "${VERSION}"

;--------------------------------
;Version specific variables

!define INSTALLER_NAME "SteamSwitch Installer.exe"
!define INSTALL_DIR "$PROGRAMFILES64\SteamSwitch"

;--------------------------------
;Build options
Unicode True
SetCompress off
Name "${APP_NAME}"
Caption "${APP_NAME}"
OutFile "${INSTALLER_NAME}"
BrandingText "${APP_NAME}"
XPStyle on
InstallDirRegKey "${REG_ROOT}" "${REG_APP_PATH}" ""
InstallDir "${INSTALL_DIR}"

RequestExecutionLevel admin
ManifestDPIAware true

;--------------------------------
;Interface Configuration

  !define MUI_ICON "img\icon.ico"
  !define MUI_UNICON "img\icon.ico"
  !define MUI_HEADERIMAGE
  !define MUI_HEADERIMAGE_BITMAP "img\HeaderImage.bmp"
  !define MUI_HEADERIMAGE_UNBITMAP "img\HeaderImage.bmp"
  !define MUI_HEADERIMAGE_BITMAP_STRETCH AspectFitHeight
  !define MUI_HEADERIMAGE_UNBITMAP_STRETCH AspectFitHeight
  !define MUI_ABORTWARNING

  !define MUI_WELCOMEFINISHPAGE_BITMAP "img\SideBanner.bmp"
  !define MUI_WELCOMEFINISHPAGE_BITMAP_NOSTRETCH
  !define MUI_UNWELCOMEFINISHPAGE_BITMAP "img\SideBanner.bmp"
  !define MUI_UNWELCOMEFINISHPAGE_BITMAP_NOSTRETCH

  !define MUI_BGCOLOR 1F212D
  !define MUI_TEXTCOLOR FFFFFF
  !define MUI_INSTFILESPAGE_COLORS "FFFFFF 1F212D"
  !define MUI_FINISHPAGE_LINK_COLOR FFAA00

;--------------------------------
;Pages

  !insertmacro MUI_PAGE_WELCOME
  !insertmacro MUI_PAGE_DIRECTORY
  !insertmacro MUI_PAGE_COMPONENTS
  !insertmacro MUI_PAGE_INSTFILES

  !define MUI_FINISHPAGE_NOAUTOCLOSE
  !define MUI_FINISHPAGE_LINK "${WEB_SITE}"
  !define MUI_FINISHPAGE_LINK_LOCATION "${WEB_SITE}"
  !define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
  !define MUI_FINISHPAGE_RUN_TEXT "Launch ${APP_NAME}"

  !insertmacro MUI_PAGE_FINISH

  !insertmacro MUI_UNPAGE_CONFIRM
  !insertmacro MUI_UNPAGE_INSTFILES

  !define MUI_UNFINISHPAGE_NOAUTOCLOSE

;--------------------------------
;Languages

  !insertmacro MUI_LANGUAGE "English"

;--------------------------------
;.onInit - Architecture and OS check

Function .onInit
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK "This product requires Windows 10 or later."
    Quit
  ${EndIf}

  ${IfNot} ${IsNativeAMD64}
    MessageBox MB_OK "This product requires a 64-bit version of Windows."
    Quit
  ${EndIf}
FunctionEnd

;--------------------------------
;Installer Sections

Section "Start Menu shortcuts" Shortcuts_StartMenu
  CreateDirectory "$SMPROGRAMS\${SM_Folder}"
  CreateShortcut "$SMPROGRAMS\${SM_Folder}\${LNK_NAME}" "$INSTDIR\${PRODUCT_EXECUTABLE}"
  CreateShortcut "$SMPROGRAMS\${SM_Folder}\${UNINSTALL_LNK_NAME}" "$INSTDIR\${UNINSTALL_EXE}"
SectionEnd

Section "Desktop shortcut" Shortcuts_Desktop
  CreateShortCut "$DESKTOP\${LNK_NAME}" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "Main files" InstSec
  SectionIn RO
  SetShellVarContext all

  SetOutPath "$INSTDIR"

  ;--- WebView2 Runtime ---
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 != ""
    Goto webview_done
  ${EndIf}

  SetDetailsPrint both
  DetailPrint "Installing: WebView2 Runtime"
  SetDetailsPrint listonly

  InitPluginsDir
  CreateDirectory "$pluginsdir\webview2bootstrapper"
  SetOutPath "$pluginsdir\webview2bootstrapper"
  File "MicrosoftEdgeWebview2Setup.exe"
  ExecWait '"$pluginsdir\webview2bootstrapper\MicrosoftEdgeWebview2Setup.exe" /silent /install'

  SetOutPath "$INSTDIR"
  webview_done:

  ;--- Extract 7z archive ---
  DetailPrint "Extracting application files..."
  SetDetailsPrint listonly
  File "${INSTALLER_7Z}"
  SetCompress auto
  Nsis7z::ExtractWithDetails "$INSTDIR\SteamSwitch.7z" "Decompressing %s..."
  Delete "$OUTDIR\SteamSwitch.7z"
  File "/oname=OPEN_SOURCE_LICENSES.txt" "${OPEN_SOURCE_LICENSES_TXT}"

  ;Store installation folder
  WriteRegStr "${REG_ROOT}" "${REG_APP_PATH}" "" $INSTDIR

  ;Create uninstaller
  WriteUninstaller "$INSTDIR\${UNINSTALL_EXE}"
  WriteRegStr HKLM "${UNINST_PATH}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKLM "${UNINST_PATH}" "DisplayVersion" "${DISPLAY_VERSION}"
  WriteRegStr HKLM "${UNINST_PATH}" "UninstallString" "$INSTDIR\${UNINSTALL_EXE}"
  WriteRegStr HKLM "${UNINST_PATH}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_PATH}" "Publisher" "${COMP_NAME}"
  WriteRegStr HKLM "${UNINST_PATH}" "HelpLink" "${WEB_SITE}"
  WriteRegStr HKLM "${UNINST_PATH}" "URLInfoAbout" "${WEB_SITE}"
  WriteRegStr HKLM "${UNINST_PATH}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
  WriteRegDWORD HKLM "${UNINST_PATH}" "NoModify" "1"
  WriteRegDWORD HKLM "${UNINST_PATH}" "NoRepair" "1"

  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINST_PATH}" "EstimatedSize" "$0"

  ;Create steamswitch:// protocol (matches winutil.RegisterProtocol)
  WriteRegStr HKCR "steamswitch" "" "URL:SteamSwitch"
  WriteRegStr HKCR "steamswitch" "URL Protocol" ""
  WriteRegStr HKCR "steamswitch\Shell\Open\Command\" "" `"$INSTDIR\${PRODUCT_EXECUTABLE}" "%1"`
SectionEnd

;--------------------------------
;Descriptions

  LangString DESC_InstSec ${LANG_ENGLISH} "All the program files"
  LangString DESC_Shortcuts_StartMenu ${LANG_ENGLISH} "Launch & Uninstall shortcuts, placed into your Start Menu."
  LangString DESC_Shortcuts_Desktop ${LANG_ENGLISH} "Shortcut to launch the program, placed onto your Desktop."

  !insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${InstSec} $(DESC_InstSec)
    !insertmacro MUI_DESCRIPTION_TEXT ${Shortcuts_StartMenu} $(DESC_Shortcuts_StartMenu)
    !insertmacro MUI_DESCRIPTION_TEXT ${Shortcuts_Desktop} $(DESC_Shortcuts_Desktop)
  !insertmacro MUI_FUNCTION_DESCRIPTION_END

;--------------------------------
;Uninstaller Section

Section "Uninstall"
  SetShellVarContext all

  RMDir /r "$INSTDIR"

  ;Remove start shortcuts
  RMDIR /r "$SMPROGRAMS\${SM_Folder}"
  ;Remove desktop shortcuts
  Delete "$DESKTOP\${LNK_NAME}"
  ;Remove uninstaller entry
  DeleteRegKey /ifempty "${REG_ROOT}" "${REG_APP_PATH}"
  DeleteRegKey HKLM "${UNINST_PATH}"
  ;Remove Protocol entry
  DeleteRegKey HKCR "steamswitch"
SectionEnd
