package main

// Win32 API 声明与常量。全部走 syscall 直接调用，无第三方依赖。

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
)

var (
	// user32
	pRegisterClassExW      = user32.NewProc("RegisterClassExW")
	pCreateWindowExW       = user32.NewProc("CreateWindowExW")
	pDefWindowProcW        = user32.NewProc("DefWindowProcW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pGetMessageW           = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessageW      = user32.NewProc("DispatchMessageW")
	pSendMessageW          = user32.NewProc("SendMessageW")
	pSetTimer              = user32.NewProc("SetTimer")
	pKillTimer             = user32.NewProc("KillTimer")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pGetWindowRect         = user32.NewProc("GetWindowRect")
	pSetWindowRgn          = user32.NewProc("SetWindowRgn")
	pShowWindow            = user32.NewProc("ShowWindow")
	pUpdateWindow          = user32.NewProc("UpdateWindow")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pDrawTextW             = user32.NewProc("DrawTextW")
	pFillRect              = user32.NewProc("FillRect")
	pGetClientRect         = user32.NewProc("GetClientRect")
	pLoadCursorW           = user32.NewProc("LoadCursorW")
	pLoadIconW             = user32.NewProc("LoadIconW")
	pLoadImageW            = user32.NewProc("LoadImageW")
	pMessageBoxW           = user32.NewProc("MessageBoxW")
	pAdjustWindowRectEx    = user32.NewProc("AdjustWindowRectEx")
	pSetCursorPos          = user32.NewProc("SetCursorPos")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenuW           = user32.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pScreenToClient        = user32.NewProc("ScreenToClient")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pIsWindowVisible       = user32.NewProc("IsWindowVisible")
	pEnumWindows           = user32.NewProc("EnumWindows")
	pGetWindowThreadProcID = user32.NewProc("GetWindowThreadProcessId")
	pPostMessageW          = user32.NewProc("PostMessageW")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pGetKeyState           = user32.NewProc("GetKeyState")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")

	// gdi32
	pCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	pCreateRoundRectRgn  = gdi32.NewProc("CreateRoundRectRgn")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pSetTextColor        = gdi32.NewProc("SetTextColor")
	pSetBkMode           = gdi32.NewProc("SetBkMode")
	pSetBkColor          = gdi32.NewProc("SetBkColor")
	pCreateFontW         = gdi32.NewProc("CreateFontW")
	pRoundRect           = gdi32.NewProc("RoundRect")
	pCreatePen           = gdi32.NewProc("CreatePen")
	pEllipse             = gdi32.NewProc("Ellipse")
	pGetDeviceCaps       = gdi32.NewProc("GetDeviceCaps")

	// kernel32
	pSetThreadExecution     = kernel32.NewProc("SetThreadExecutionState")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW        = kernel32.NewProc("Process32FirstW")
	pProcess32NextW         = kernel32.NewProc("Process32NextW")
	pCloseHandle            = kernel32.NewProc("CloseHandle")

	// advapi32（开机自启）
	pRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey     = advapi32.NewProc("RegCloseKey")

	// shell32（托盘气泡/图标）
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
)

const (
	WS_POPUP        = 0x80000000
	WS_CHILD        = 0x40000000
	WS_VISIBLE      = 0x10000000
	WS_BORDER       = 0x00800000
	WS_TABSTOP      = 0x00010000
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_APPWINDOW  = 0x00010000
	WS_EX_TOPMOST   = 0x00000008
	ES_NUMBER       = 0x2000
	ES_AUTOHSCROLL  = 0x0080
	ES_CENTER       = 0x0001
	CS_HREDRAW      = 0x0002
	CS_VREDRAW      = 0x0001

	WM_CREATE      = 0x0001
	WM_DESTROY     = 0x0002
	WM_CLOSE       = 0x0010
	WM_PAINT       = 0x000F
	WM_ERASEBKGND  = 0x0014
	WM_TIMER       = 0x0113
	WM_COMMAND     = 0x0111
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONUP   = 0x0205
	WM_MOUSEMOVE   = 0x0200
	WM_NCHITTEST   = 0x0084
	WM_SETFONT     = 0x0030
	WM_CTLCOLOREDIT = 0x0133
	WM_CTLCOLORSTATIC = 0x0138
	WM_APP         = 0x8000
	WM_TRAY        = WM_APP + 1
	WM_SYSCOMMAND  = 0x0112
	SC_MINIMIZE    = 0xF020
	HTCLIENT       = 1
	HTCAPTION      = 2
	HTCAPTION_AREA = 58

	SW_HIDE = 0
	SW_SHOW = 5

	COLOR_WINDOW_BRUSH = 6
	DEFAULT_GUI_FONT   = 17
	IDC_ARROW          = 32512
	IDI_APPLICATION    = 32512

	TRANSPARENT = 1

	DEFAULT_CHARSET      = 1
	OUT_TT_PRECIS        = 4
	CLIP_DEFAULT_PRECIS  = 0
	CLEARTYPE_QUALITY    = 5
	DEFAULT_PITCH        = 0
	FF_DONTCARE          = 0
	LOGPIXELSX           = 88

	DT_LEFT        = 0x0000
	DT_CENTER      = 0x0001
	DT_RIGHT       = 0x0002
	DT_VCENTER     = 0x0004
	DT_SINGLELINE  = 0x0020
	DT_NOPREFIX    = 0x0800
	DT_END_ELLIPSIS = 0x8000

	TH32CS_SNAPPROCESS = 0x00000002

	NIM_ADD    = 0
	NIM_MODIFY = 1
	NIM_DELETE = 2
	NIF_MESSAGE = 1
	NIF_ICON    = 2
	NIF_TIP     = 4

	MF_STRING    = 0x0000
	MF_GRAYED    = 0x0001
	MF_CHECKED   = 0x0008
	MF_SEPARATOR = 0x0800
	MF_ENABLED   = 0x0000

	TPM_LEFTALIGN = 0x0000
	TPM_RIGHTBUTTON = 0x0002
	TPM_NONOTIFY    = 0x0080

	HKEY_CURRENT_USER = 0x80000001
	KEY_SET_VALUE     = 0x0002
	KEY_QUERY_VALUE   = 0x0001
	REG_SZ            = 1

	IMAGE_ICON = 1
	LR_LOADFROMFILE = 0x0000
	LR_DEFAULTSIZE  = 0x0040

	// 界面配色（COLORREF = 0x00BBGGRR）
	colorBg      uint32 = 0x001D1816 // #16181D
	colorCard    uint32 = 0x0027201E // #1E2027
	colorCard2   uint32 = 0x002C2A29 // #292A2C
	colorText    uint32 = 0x00EDEAE8 // #E8EAED
	colorSub     uint32 = 0x00A6A09A // #9AA0A6
	colorDim     uint32 = 0x00706A66 // #666A70
	colorBlue    uint32 = 0x00F6823B // #3B82F6
	colorBlueDim uint32 = 0x004E3C2E // #2E3C4E
	colorGreen   uint32 = 0x005EC522 // #22C55E
	colorGreenDim uint32 = 0x003A4531 // #31453A
	colorRed     uint32 = 0x004444EF // #EF4444
	colorDivider uint32 = 0x00342D2A // #2A2D34
	colorChip    uint32 = 0x0032302F // #2F3032
)

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left, Top, Right, Bottom int32
}

type msgStruct struct {
	Hwnd     uintptr
	Message  uint32
	_        uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	_        uint32
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

func nowMillis() int64 { return time.Now().UnixMilli() }

func loword(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func hiword(v uintptr) uint16 { return uint16((v >> 16) & 0xFFFF) }

func utf16Ptr(s string) uintptr {
	p, _ := syscall.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

func setWindowText(h uintptr, s string) {
	pSetWindowTextW.Call(h, utf16Ptr(s))
}

func getWindowText(h uintptr) string {
	buf := make([]uint16, 128)
	n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}
