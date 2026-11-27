package main

// 界面绘制、托盘、开机自启、手动输入小窗。全部自绘，视觉统一（深色 + 圆角）。

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	winW = 380
	winH = 622
)

var (
	scaleF float64 = 1.0

	fontTitle  uintptr
	fontBig    uintptr
	fontBold   uintptr
	fontNormal uintptr
	fontSmall  uintptr
	fontMicro  uintptr

	hEditMinutes uintptr

	hits       []hitBox
	hoverID    string
	pressedID  string
	hoverEdit  bool
	darkBrush  uintptr
	trayAdded  bool
	hIcon      uintptr
	hFontEdit  uintptr
)

type hitBox struct {
	ID           string
	X, Y, W, H   int
}

func sc(v int) int { return int(float64(v)*scaleF) }

func newFont(pixels int, weight int) uintptr {
	face := utf16Ptr("Microsoft YaHei UI")
	h, _, _ := pCreateFontW.Call(
		uintptr(-pixels), 0, 0, 0,
		uintptr(weight), 0, 0, 0,
		DEFAULT_CHARSET, OUT_TT_PRECIS, CLIP_DEFAULT_PRECIS,
		CLEARTYPE_QUALITY, DEFAULT_PITCH|FF_DONTCARE,
		face,
	)
	return h
}

func initFonts() {
	hdc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, LOGPIXELSX)
	pReleaseDC.Call(0, hdc)
	if dpi == 0 {
		dpi = 96
	}
	scaleF = float64(dpi) / 96.0

	fontTitle = newFont(sc(20), 600)
	fontBig = newFont(sc(34), 700)
	fontBold = newFont(sc(13), 700)
	fontNormal = newFont(sc(13), 400)
	fontSmall = newFont(sc(12), 400)
	fontMicro = newFont(sc(11), 400)
	hFontEdit = newFont(sc(13), 400)

	darkBrush, _, _ = pCreateSolidBrush.Call(uintptr(colorCard))
}

func fillRect(hdc uintptr, x, y, w, h int, color uint32) {
	br, _, _ := pCreateSolidBrush.Call(uintptr(color))
	rc := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), br)
	pDeleteObject.Call(br)
}

func roundRect(hdc uintptr, x, y, w, h, radius int, color uint32) {
	br, _, _ := pCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := pCreatePen.Call(0, 0, uintptr(color)) // PS_SOLID
	oldBr, _, _ := pSelectObject.Call(hdc, br)
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc,
		uintptr(x), uintptr(y), uintptr(x+w), uintptr(y+h),
		uintptr(radius), uintptr(radius))
	pSelectObject.Call(hdc, oldBr)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(br)
	pDeleteObject.Call(pen)
}

func dot(hdc uintptr, cx, cy, r int, color uint32) {
	br, _, _ := pCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := pCreatePen.Call(0, 0, uintptr(color))
	oldBr, _, _ := pSelectObject.Call(hdc, br)
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pEllipse.Call(hdc, uintptr(cx-r), uintptr(cy-r), uintptr(cx+r), uintptr(cy+r))
	pSelectObject.Call(hdc, oldBr)
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(br)
	pDeleteObject.Call(pen)
}

func drawText(hdc uintptr, s string, x, y, w, h int, font uintptr, color uint32, flags uint32) {
	u, _ := syscall.UTF16FromString(s)
	rc := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
	pSelectObject.Call(hdc, font)
	pSetTextColor.Call(hdc, uintptr(color))
	pSetBkMode.Call(hdc, uintptr(TRANSPARENT))
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&rc)), uintptr(flags))
}

func lighten(c uint32, delta int32) uint32 {
	r := int32(c & 0xFF)
	g := int32((c >> 8) & 0xFF)
	b := int32((c >> 16) & 0xFF)
	clamp := func(v int32) uint32 {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint32(v)
	}
	return clamp(r+delta) | clamp(g+delta)<<8 | clamp(b+delta)<<16
}

func drawButton(hdc uintptr, id string, x, y, w, h int, label string, bg, fg uint32, font uintptr) {
	actual := bg
	if pressedID == id {
		actual = lighten(bg, -18)
	} else if hoverID == id {
		actual = lighten(bg, 16)
	}
	roundRect(hdc, x, y, w, h, sc(10), actual)
	drawText(hdc, label, x, y, w, h, font, fg, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	hits = append(hits, hitBox{id, x, y, w, h})
}

func fmtRemain(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func maskMinutes() string {
	s := st.minutes
	if s > 0 {
		return strconv.Itoa(s)
	}
	return ""
}

func drawUI(hdc uintptr, width, height int) {
	hits = hits[:0]
	fillRect(hdc, 0, 0, width, height, colorBg)

	// 顶部
	drawText(hdc, "AntiSleep", sc(24), sc(14), sc(200), sc(28), fontTitle, colorText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText(hdc, "防息屏 · 后台守护", sc(24), sc(42), sc(220), sc(18), fontMicro, colorSub, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	// 关闭（隐藏到托盘）
	cx := width - sc(44)
	roundRect(hdc, cx, sc(12), sc(28), sc(28), sc(8), func() uint32 {
		if hoverID == "close" {
			return lighten(colorCard, 10)
		}
		return colorCard
	}())
	drawText(hdc, "×", cx, sc(12), sc(28), sc(28), fontNormal, colorSub, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	hits = append(hits, hitBox{"close", cx, sc(12), sc(28), sc(28)})

	// 状态
	status, statusColor := statusText()
	dot(hdc, sc(28), sc(80), sc(4), statusColor)
	drawText(hdc, status, sc(40), sc(70), width-sc(90), sc(20), fontSmall, colorSub, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)

	// 倒计时
	var big string
	bigColor := colorText
	if st.running {
		if st.infinite {
			big = "∞"
		} else {
			big = fmtRemain(st.remainSec)
		}
		bigColor = colorGreen
	} else if len(st.activeWhitelist) > 0 {
		big = "守 护"
		bigColor = colorBlue
	} else {
		big = "--:--"
		bigColor = colorDim
	}
	drawText(hdc, big, sc(24), sc(96), width-sc(48), sc(52), fontBig, bigColor, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// 副说明
	var tip string
	switch {
	case st.running && st.infinite:
		tip = "持续防息屏，直到手动停止"
	case st.running:
		tip = "剩余时间 · 到时自动停止"
	case len(st.activeWhitelist) > 0:
		tip = fmt.Sprintf("守护中：%d 个指定应用在后台运行", len(st.activeWhitelist))
	default:
		tip = "设置时长后点开始，或添加守护应用"
	}
	drawText(hdc, tip, sc(24), sc(150), width-sc(48), sc(18), fontMicro, colorSub, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	// 时长输入行
	y := sc(184)
	editX, editY := sc(24), y
	editW, editH := sc(72), sc(32)
	roundRect(hdc, editX, editY, editW, editH, sc(8), colorCard2)
	// EDIT 控件由系统绘制，位置在 main.go 里同步
	drawText(hdc, "分钟", editX+editW+sc(8), y, sc(34), editH, fontMicro, colorSub, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	chips := []struct {
		id, label string
		val       int
	}{
		{"m30", "30", 30},
		{"m60", "60", 60},
		{"m120", "120", 120},
		{"inf", "∞", 0},
	}
	chipX := editX + editW + sc(46)
	for _, c := range chips {
		active := false
		if c.id == "inf" {
			active = st.infinite && st.running
		} else if !st.running && st.minutes == c.val {
			active = true
		}
		bg := colorChip
		if active {
			bg = colorBlueDim
		}
		roundRect(hdc, chipX, y, sc(48), sc(30), sc(8), func() uint32 {
			if hoverID == c.id {
				return lighten(bg, 12)
			}
			return bg
		}())
		fg := colorSub
		if active {
			fg = colorBlue
		}
		drawText(hdc, c.label, chipX, y, sc(48), sc(30), fontMicro, fg, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		hits = append(hits, hitBox{c.id, chipX, y, sc(48), sc(30)})
		chipX += sc(56)
	}

	// 主按钮
	by := sc(230)
	if st.running {
		drawButton(hdc, "stop", sc(24), by, sc(196), sc(44), "停 止", colorRed, 0x00FFFFFF, fontBold)
	} else {
		drawButton(hdc, "start", sc(24), by, sc(196), sc(44), "开始防息屏", colorBlue, 0x00FFFFFF, fontBold)
	}
	drawButton(hdc, "pause", sc(232), by, width-sc(24)-sc(232), sc(44), func() string {
		if st.running {
			return "清零"
		}
		return "退出"
	}(), colorCard, colorSub, fontNormal)

	// 分隔
	fillRect(hdc, sc(24), sc(292), width-sc(48), sc(1), colorDivider)

	// 守护应用区
	drawText(hdc, "后台守护应用", sc(24), sc(302), sc(200), sc(20), fontBold, colorText, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawButton(hdc, "add", width-sc(24)-sc(76), sc(298), sc(76), sc(26), "＋ 添加", colorCard, colorSub, fontMicro)
	drawText(hdc, "这些应用运行时，即使锁屏也不休眠", sc(24), sc(324), width-sc(48), sc(16), fontMicro, colorDim, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	// 列表
	listY := sc(346)
	if len(st.whitelist) == 0 {
		drawText(hdc, "还没有守护应用", sc(24), listY, width-sc(48), sc(24), fontMicro, colorDim, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	} else {
		active := make(map[string]bool)
		for _, a := range st.activeWhitelist {
			active[strings.ToLower(a)] = true
		}
		shown := len(st.whitelist)
		if shown > 5 {
			shown = 5
		}
		for i := 0; i < shown; i++ {
			name := st.whitelist[i]
			rowY := listY + i*sc(34)
			roundRect(hdc, sc(24), rowY, width-sc(48), sc(30), sc(8), colorCard)
			on := active[strings.ToLower(name)]
			c := colorDim
			if on {
				c = colorGreen
			}
			dot(hdc, sc(40), rowY+sc(15), sc(3), c)
			label := strings.TrimSuffix(name, ".exe")
			drawText(hdc, label, sc(52), rowY, width-sc(100), sc(30), fontMicro, func() uint32 {
				if on {
					return colorText
				}
				return colorSub
			}(), DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)
			// 删除
			delX := width - sc(24) - sc(24)
			drawText(hdc, "×", delX, rowY, sc(24), sc(30), fontMicro, colorDim, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			hits = append(hits, hitBox{"del:" + name, delX, rowY, sc(24), sc(30)})
		}
		if len(st.whitelist) > 5 {
			drawText(hdc, fmt.Sprintf("…另有 %d 个，先删掉上面任一项即可露出", len(st.whitelist)-5), sc(24), listY+5*sc(34), width-sc(48), sc(14), fontMicro, colorDim, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		}
	}

	// 底部：开机自启 + 合盖 + 版本
	ay := sc(532)
	boxSize := sc(16)
	roundRect(hdc, sc(24), ay, boxSize, boxSize, sc(4), colorCard2)
	if st.autostart {
		roundRect(hdc, sc(24), ay, boxSize, boxSize, sc(4), colorBlue)
		drawText(hdc, "✓", sc(24), ay, boxSize, boxSize, fontMicro, 0x00FFFFFF, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "开机自启（静默到托盘）", sc(46), ay, sc(200), boxSize, fontMicro, colorSub, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	hits = append(hits, hitBox{"autostart", sc(24), ay - sc(4), sc(200), boxSize + sc(8)})
	// 合盖不睡眠
	ly := sc(562)
	roundRect(hdc, sc(24), ly, boxSize, boxSize, sc(4), colorCard2)
	if st.lidMode {
		roundRect(hdc, sc(24), ly, boxSize, boxSize, sc(4), colorBlue)
		drawText(hdc, "✓", sc(24), ly, boxSize, boxSize, fontMicro, 0x00FFFFFF, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	drawText(hdc, "合盖也不休眠（需管理员授权）", sc(46), ly, sc(250), boxSize, fontMicro, colorSub, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	hits = append(hits, hitBox{"lid", sc(24), ly - sc(4), sc(250), boxSize + sc(8)})

	drawText(hdc, "v1.2.0", width-sc(94), ay, sc(70), boxSize, fontMicro, colorDim, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)

	// Toast
	if st.toast != "" && nowMillis() < st.toastUntil {
		drawText(hdc, st.toast, sc(24), sc(596), width-sc(48), sc(18), fontMicro, colorBlue, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	}
}

// ---- 交互 ----

func hitTest(x, y int) string {
	// 后加入的在上层
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H {
			return h.ID
		}
	}
	return ""
}

func handleClick(id string) {
	switch {
	case id == "close":
		pShowWindow.Call(hwndMain, SW_HIDE)
	case id == "start":
		minutes, err := strconv.Atoi(getWindowText(hEditMinutes))
		if err != nil || minutes < 1 {
			showToast("请输入 1–1440 的分钟数")
			return
		}
		startRun(false, minutes)
	case id == "stop":
		stopRun()
	case id == "pause":
		if st.running {
			stopRun()
		} else {
			confirmLidRestore()
			removeTrayIcon()
			pDestroyWindow.Call(hwndMain)
		}
	case id == "m30", id == "m60", id == "m120":
		st.minutes, _ = strconv.Atoi(strings.TrimPrefix(id, "m"))
		setWindowText(hEditMinutes, strconv.Itoa(st.minutes))
		saveConfig()
		repaint()
	case id == "inf":
		if st.running {
			stopRun()
		}
		startRun(true, 0)
	case id == "add":
		showAddMenu()
	case id == "lid":
		toggleLid()
	case id == "autostart":
		st.autostart = !st.autostart
		setAutostart(st.autostart)
		repaint()
	case strings.HasPrefix(id, "del:"):
		removeWhitelist(strings.TrimPrefix(id, "del:"))
		repaint()
	}
}

func repaint() {
	pInvalidateRect.Call(hwndMain, 0, 0)
}

// ---- 托盘 ----

func addTrayIcon(hwnd uintptr) {
	if trayAdded {
		return
	}
	var nid notifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAY
	nid.HIcon = hIcon
	tip, _ := syscall.UTF16FromString("AntiSleep · 防息屏助手")
	copy(nid.SzTip[:], tip)
	r, _, _ := pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
	if r != 0 {
		trayAdded = true
	}
}

func removeTrayIcon() {
	if !trayAdded {
		return
	}
	var nid notifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwndMain
	nid.UID = 1
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	trayAdded = false
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	_, _, _ = pAppendMenuW.Call(menu, MF_STRING, 1001, utf16Ptr("显示窗口"))
	_, _, _ = pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	if st.running {
		_, _, _ = pAppendMenuW.Call(menu, MF_STRING, 1003, utf16Ptr("停止防息屏"))
	} else {
		_, _, _ = pAppendMenuW.Call(menu, MF_STRING, 1002, utf16Ptr("开始防息屏（用当前时长）"))
	}
	flag := uintptr(MF_STRING)
	if st.autostart {
		flag |= MF_CHECKED
	}
	_, _, _ = pAppendMenuW.Call(menu, flag, 1004, utf16Ptr("开机自启"))
	_, _, _ = pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	_, _, _ = pAppendMenuW.Call(menu, MF_STRING, 1005, utf16Ptr("退出"))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	_, _, _ = pTrackPopupMenu.Call(menu, TPM_LEFTALIGN|TPM_RIGHTBUTTON|TPM_NONOTIFY, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pDestroyMenu.Call(menu)
	pPostMessageW.Call(hwnd, 0, 0, 0)
}

func handleTrayCommand(id int) {
	switch id {
	case 1001:
		pShowWindow.Call(hwndMain, SW_SHOW)
		pSetForegroundWindow.Call(hwndMain)
	case 1002:
		minutes, err := strconv.Atoi(getWindowText(hEditMinutes))
		if err != nil || minutes < 1 {
			minutes = st.minutes
		}
		startRun(false, minutes)
	case 1003:
		stopRun()
	case 1004:
		st.autostart = !st.autostart
		setAutostart(st.autostart)
		repaint()
	case 1005:
		confirmLidRestore()
		removeTrayIcon()
		pDestroyWindow.Call(hwndMain)
	}
}

// ---- 添加守护应用 ----

func showAddMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	list := windowedProcesses()
	_, _, _ = pAppendMenuW.Call(menu, MF_STRING, 2001, utf16Ptr("手动输入进程名…"))
	_, _, _ = pAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	if len(list) == 0 {
		_, _, _ = pAppendMenuW.Call(menu, MF_GRAYED, 0, utf16Ptr("（没有检测到带窗口的程序）"))
	}
	if len(list) > 18 {
		list = list[:18]
	}
	for i, name := range list {
		label := strings.TrimSuffix(name, ".exe")
		_, _, _ = pAppendMenuW.Call(menu, MF_STRING, uintptr(2100+i), utf16Ptr(label))
	}

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwndMain)
	choice, _, _ := pTrackPopupMenu.Call(menu, TPM_LEFTALIGN|TPM_RIGHTBUTTON|TPM_NONOTIFY, uintptr(pt.X), uintptr(pt.Y), 0, hwndMain, 0)
	pDestroyMenu.Call(menu)
	pPostMessageW.Call(hwndMain, 0, 0, 0)

	id := int(choice)
	switch {
	case id == 2001:
		openInputDialog()
	case id >= 2100 && id < 2100+len(list):
		addWhitelist(list[id-2100])
		repaint()
	}
}

// ---- 手动输入小窗 ----

var (
	dlgHwnd, dlgEdit, dlgOk uintptr
	dlgFont                uintptr
)

func openInputDialog() {
	if dlgHwnd != 0 {
		pShowWindow.Call(dlgHwnd, SW_SHOW)
		pSetForegroundWindow.Call(dlgHwnd)
		return
	}
	className := utf16Ptr("AntiSleepDlg")
	title := utf16Ptr("添加守护应用")
	style := uintptr(0x00C00000 | 0x00800000 | 0x00080000) // WS_POPUP|WS_CAPTION|WS_SYSMENU
	dw := sc(320)
	dh := sc(158)
	var wr rect
	pGetWindowRect.Call(hwndMain, uintptr(unsafe.Pointer(&wr)))
	cx := int(wr.Left+wr.Right) / 2
	cy := int(wr.Top+wr.Bottom) / 2
	dlgHwnd, _, _ = pCreateWindowExW.Call(
		WS_EX_TOOLWINDOW, className, title, style,
		uintptr(cx-dw/2), uintptr(cy-dh/2), uintptr(dw), uintptr(dh),
		hwndMain, 0, 0, 0,
	)
	if dlgHwnd == 0 {
		return
	}
	dlgFont = newFont(sc(13), 400)
	// 说明
	stText := utf16Ptr("输入进程名，例如 chrome.exe、AfterFX.exe")
	pCreateWindowExW.Call(0, utf16Ptr("STATIC"), stText,
		uintptr(WS_CHILD|WS_VISIBLE), uintptr(sc(18)), uintptr(sc(14)), uintptr(sc(280)), uintptr(sc(20)),
		dlgHwnd, 0, 0, 0)

	dlgEdit, _, _ = pCreateWindowExW.Call(
		0x00000200, utf16Ptr("EDIT"), utf16Ptr(""),
		uintptr(WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0080), // ES_AUTOHSCROLL
		uintptr(sc(18)), uintptr(sc(44)), uintptr(sc(284)), uintptr(sc(28)),
		dlgHwnd, 3001, 0, 0,
	)
	pSendMessageW.Call(dlgEdit, WM_SETFONT, dlgFont, 1)

	dlgOk, _, _ = pCreateWindowExW.Call(0, utf16Ptr("BUTTON"), utf16Ptr("添加"),
		uintptr(WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0001|0x00000001), // BS_DEFPUSHBUTTON
		uintptr(sc(196)), uintptr(sc(92)), uintptr(sc(106)), uintptr(sc(34)), dlgHwnd, 3002, 0, 0)
	pSendMessageW.Call(dlgOk, WM_SETFONT, dlgFont, 1)

	btnCancel, _, _ := pCreateWindowExW.Call(0, utf16Ptr("BUTTON"), utf16Ptr("取消"),
		uintptr(WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00000000),
		uintptr(sc(84)), uintptr(sc(92)), uintptr(sc(106)), uintptr(sc(34)), dlgHwnd, 3003, 0, 0)
	pSendMessageW.Call(btnCancel, WM_SETFONT, dlgFont, 1)

	pShowWindow.Call(dlgHwnd, SW_SHOW)
	pSetForegroundWindow.Call(dlgHwnd)
}

func inputDialogProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch loword(wparam) {
		case 3002:
			txt := strings.TrimSpace(getWindowText(dlgEdit))
			if txt != "" {
				addWhitelist(txt)
				setWindowText(dlgEdit, "")
				repaint()
				pShowWindow.Call(hwnd, SW_HIDE)
			}
			return 0
		case 3003:
			pShowWindow.Call(hwnd, SW_HIDE)
			return 0
		}
		return 0
	case WM_CLOSE:
		pShowWindow.Call(hwnd, SW_HIDE)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

// ---- 开机自启 ----

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func exeCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return "\"" + exe + "\" --tray"
}

func queryAutostart() bool {
	var hkey uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, utf16Ptr(runKeyPath), 0, KEY_QUERY_VALUE, uintptr(unsafe.Pointer(&hkey)))
	if r != 0 {
		return false
	}
	defer pRegCloseKey.Call(hkey)
	var typ uint32
	var size uint32 = 512
	buf := make([]uint16, 256)
	r, _, _ = pRegQueryValueExW.Call(hkey, utf16Ptr("AntiSleep"), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	return r == 0 && typ == REG_SZ
}

func setAutostart(on bool) {
	var hkey uintptr
	r, _, _ := pRegOpenKeyExW.Call(HKEY_CURRENT_USER, utf16Ptr(runKeyPath), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&hkey)))
	if r != 0 {
		showToast("写入注册表失败，可能被安全软件拦截")
		return
	}
	defer pRegCloseKey.Call(hkey)
	if on {
		cmd := exeCommand()
		if cmd == "" {
			showToast("获取程序路径失败")
			return
		}
		s := syscall.StringToUTF16(cmd)
		r, _, _ = pRegSetValueExW.Call(hkey, utf16Ptr("AntiSleep"), 0, REG_SZ,
			uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)*2))
		if r != 0 {
			showToast("设置开机自启失败")
			st.autostart = false
		}
	} else {
		pRegDeleteValueW.Call(hkey, utf16Ptr("AntiSleep"))
	}
}
