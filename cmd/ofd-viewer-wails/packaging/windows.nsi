; 当前用户安装，不覆盖 Windows UserChoice，也不强制抢占 .ofd 默认关联。
Unicode true
!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!include "MUI2.nsh"
Name "OFD Reader (Wails)"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\zc310\ofd-viewer-wails"
InstallDirRegKey HKCU "Software\zc310\OFD Reader Wails" "InstallDir"
RequestExecutionLevel user
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

Section "OFD Reader"
  SetOutPath "$INSTDIR"
  File /oname=ofd-viewer-wails.exe "${BINARY}"
  File "icon.ico"
  File /oname=README.md "..\README.md"
  File "..\..\..\LICENSE"
  File "..\..\..\NOTICE"
  File "..\..\..\THIRD-PARTY-LICENSES"
  File /oname=ADOBE-GB1-NOTICE "..\..\..\internal\cidmap\NOTICE"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\zc310\OFD Reader Wails" "InstallDir" "$INSTDIR"
  CreateShortcut "$SMPROGRAMS\OFD Reader (Wails).lnk" "$INSTDIR\ofd-viewer-wails.exe" "" "$INSTDIR\icon.ico" 0
  WriteRegStr HKCU "Software\Classes\OFD.Wails.Document" "" "OFD document"
  WriteRegStr HKCU "Software\Classes\OFD.Wails.Document\Application" "ApplicationName" "OFD Reader (Wails)"
  WriteRegStr HKCU "Software\Classes\OFD.Wails.Document\DefaultIcon" "" '$\"$INSTDIR\icon.ico$\",0'
  WriteRegStr HKCU "Software\Classes\OFD.Wails.Document\shell\open\command" "" '$\"$INSTDIR\ofd-viewer-wails.exe$\" -- $\"%1$\"'
  WriteRegStr HKCU "Software\Classes\.ofd\OpenWithProgids" "OFD.Wails.Document" ""
  WriteRegStr HKCU "Software\zc310\OFD Reader Wails\Capabilities" "ApplicationName" "OFD Reader (Wails)"
  WriteRegStr HKCU "Software\zc310\OFD Reader Wails\Capabilities" "ApplicationDescription" "View OFD documents"
  WriteRegStr HKCU "Software\zc310\OFD Reader Wails\Capabilities" "ApplicationIcon" '$\"$INSTDIR\icon.ico$\",0'
  WriteRegStr HKCU "Software\zc310\OFD Reader Wails\Capabilities\FileAssociations" ".ofd" "OFD.Wails.Document"
  WriteRegStr HKCU "Software\RegisteredApplications" "OFD Reader (Wails)" "Software\zc310\OFD Reader Wails\Capabilities"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OFD Reader Wails" "DisplayName" "OFD Reader (Wails)"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OFD Reader Wails" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OFD Reader Wails" "DisplayIcon" '$\"$INSTDIR\icon.ico$\",0'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OFD Reader Wails" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd

Section "Uninstall"
  ReadRegStr $0 HKCU "Software\zc310\OFD Reader Wails" "InstallDir"
  StrCmp $0 "$INSTDIR" 0 skip_registry_cleanup
  Delete "$SMPROGRAMS\OFD Reader (Wails).lnk"
  DeleteRegKey HKCU "Software\Classes\OFD.Wails.Document"
  DeleteRegValue HKCU "Software\Classes\.ofd\OpenWithProgids" "OFD.Wails.Document"
  DeleteRegValue HKCU "Software\RegisteredApplications" "OFD Reader (Wails)"
  DeleteRegKey HKCU "Software\zc310\OFD Reader Wails"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\OFD Reader Wails"
skip_registry_cleanup:
  Delete "$INSTDIR\ofd-viewer-wails.exe"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\NOTICE"
  Delete "$INSTDIR\THIRD-PARTY-LICENSES"
  Delete "$INSTDIR\ADOBE-GB1-NOTICE"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd
