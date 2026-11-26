package main

// 入口：无边框深色窗口、消息循环、绘制调度。

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	hwndMain uintptr
	editBrush uintptr
	editRect  rect
)

func main() {
	pSetProcessDPIAware.Call()
	loadConfig()

	trayMode := false
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, "--tray") || strings.EqualFold(a, "/tray") {
			trayMode = true
		}
	}

	hInst, _, _ := pGetModuleHandleW.Call(0)
	hIcon, _, _ = pLoadIconW.Call(0, IDI_APPLICATION)

	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)

	className, _ := syscall.UTF16PtrFromString("AntiSleepWnd")
	title, _ := syscall.UTF16PtrFromString("AntiSleep · 防息屏助手")
	classNameW := uintptr(unsafe.Pointer(className))
	titleW := uintptr(unsafe.Pointer(title))

	wc := wndClassEx{
		Style:         CS_HREDRAW | CS_VREDRAW,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hInst,
		HIcon:         hIcon,
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: className,
		HIconSm:       hIcon,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		messageBox("窗口类注册失败，程序无法启动。")
		os.Exit(1)
	}

	// 手动输入对话框也注册一个类
	dlgClass, _ := syscall.UTF16PtrFromString("AntiSleepDlg")
	dwc := wndClassEx{
		Style:         CS_HREDRAW | CS_VREDRAW,
		LpfnWndProc:   syscall.NewCallback(inputDialogProc),
		HInstance:     hInst,
		HIcon:         hIcon,
		HCursor:       cursor,
		HbrBackground: COLOR_WINDOW_BRUSH,
		LpszClassName: dlgClass,
		HIconSm:       hIcon,
	}
	dwc.CbSize = uint32(unsafe.Sizeof(dwc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&dwc)))

	// 先取得 DPI 再算窗口尺寸：initFonts 里会设置 scaleF
	initFonts()
	editBrush, _, _ = pCreateSolidBrush.Call(uintptr(colorCard2))

	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	w := sc(winW)
	h := sc(winH)
	x := (int(sw) - w) / 2
	y := (int(sh) - h) / 3

	hwndMain, _, _ = pCreateWindowExW.Call(
		WS_EX_APPWINDOW, classNameW, titleW,
		uintptr(WS_POPUP),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, hInst, 0,
	)
	if hwndMain == 0 {
		messageBox("主窗口创建失败，程序无法启动。")
		os.Exit(1)
	}

	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(w), uintptr(h), uintptr(sc(18)), uintptr(sc(18)))
	pSetWindowRgn.Call(hwndMain, rgn, 1)

	if trayMode {
		pShowWindow.Call(hwndMain, SW_HIDE)
	} else {
		pShowWindow.Call(hwndMain, SW_SHOW)
		pSetForegroundWindow.Call(hwndMain)
	}
	pUpdateWindow.Call(hwndMain)

	pSetTimer.Call(hwndMain, 1, 1000, 0)

	var m msgStruct
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 || ret == 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	releaseExecutionState()
}

func messageBox(text string) {
	cap := utf16Ptr("AntiSleep")
	pMessageBoxW.Call(0, utf16Ptr(text), cap, 0x10)
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		hwndMain = hwnd
		createEdit(hwnd)
		addTrayIcon(hwnd)
		return 0

	case WM_PAINT:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		drawUI(hdc, sc(winW), sc(winH))
		syncEditPosition()
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0

	case WM_ERASEBKGND:
		return 1

	case WM_CTLCOLOREDIT:
		pSetTextColor.Call(wparam, uintptr(colorText))
		pSetBkColor.Call(wparam, uintptr(colorCard2))
		return editBrush

	case WM_MOUSEMOVE:
		id := hitTest(int(wparam), int(lparam))
		if id != hoverID {
			hoverID = id
			repaint()
		}
		return 0

	case WM_LBUTTONDOWN:
		pressedID = hitTest(int(wparam), int(lparam))
		repaint()
		return 0

	case WM_LBUTTONUP:
		id := hitTest(int(wparam), int(lparam))
		if id != "" && id == pressedID {
			handleClick(id)
		}
		pressedID = ""
		hoverID = id
		repaint()
		return 0

	case WM_NCHITTEST:
		{
			var wr rect
			pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
			sx := int32(int16((lparam >> 16) & 0xFFFF))
			sy := int32(int16(lparam & 0xFFFF))
			relY := sy - wr.Top
			relX := sx - wr.Left
			if relY >= 0 && relY < int32(sc(58)) && relX < int32(sc(winW)-44) && relX > int32(sc(20)) {
				return HTCAPTION
			}
			return HTCLIENT
		}

	case WM_TIMER:
		if wparam == 1 {
			stepSecond()
			repaint()
		}
		return 0

	case WM_TRAY:
		switch lparam {
		case 0x0202: // WM_LBUTTONUP
			pShowWindow.Call(hwnd, SW_SHOW)
			pSetForegroundWindow.Call(hwnd)
		case 0x0205: // WM_RBUTTONUP
			showTrayMenu(hwnd)
		}
		return 0

	case WM_SYSCOMMAND:
		if wparam == SC_MINIMIZE {
			pShowWindow.Call(hwnd, SW_HIDE)
			return 0
		}
		return 0

	case WM_CLOSE:
		// 关窗=收进托盘，防息屏继续生效；要彻底退出请用托盘菜单
		pShowWindow.Call(hwnd, SW_HIDE)
		return 0

	case WM_DESTROY:
		pKillTimer.Call(hwnd, 1)
		removeTrayIcon()
		releaseExecutionState()
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func createEdit(parent uintptr) {
	hEditMinutes, _, _ = pCreateWindowExW.Call(
		0, utf16Ptr("EDIT"), utf16Ptr(strconv.Itoa(st.minutes)),
		uintptr(WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_NUMBER|ES_CENTER|ES_AUTOHSCROLL),
		uintptr(sc(24)), uintptr(sc(186)), uintptr(sc(72)), uintptr(sc(30)),
		parent, 1001, 0, 0,
	)
	pSendMessageW.Call(hEditMinutes, WM_SETFONT, hFontEdit, 1)
	editRect = rect{int32(sc(24)), int32(sc(186)), int32(sc(24) + sc(72)), int32(sc(186) + sc(30))}
}

func syncEditPosition() {
	if hEditMinutes == 0 {
		return
	}
	next := rect{int32(sc(24)), int32(sc(186)), int32(sc(24) + sc(72)), int32(sc(186) + sc(30))}
	if next == editRect {
		return
	}
	editRect = next
	pSetWindowPos.Call(hEditMinutes, 0,
		uintptr(next.Left), uintptr(next.Top), uintptr(next.Right - next.Left), uintptr(next.Bottom - next.Top),
		uintptr(0x0004|0x0010)) // SWP_NOZORDER|SWP_NOACTIVATE
}
