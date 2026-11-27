package main

// 合盖不睡眠：笔记本合上盖子后系统默认会睡眠，把后台任务挂起。
// Windows 的合盖动作由电源计划（SUB_BUTTONS / LIDACTION）控制，
// 改成 0（不采取任何操作）就能让合盖后继续跑，代价是需要管理员权限。

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func runAsAdmin(file, params string) bool {
	r, _, _ := pShellExecuteW.Call(0,
		utf16Ptr("runas"),
		utf16Ptr(file),
		utf16Ptr(params),
		0,
		uintptr(1), // SW_SHOWNORMAL
	)
	return r > 32
}

// lidAction: 0=不采取任何操作(不睡) 1=睡眠 2=休眠 3=关机
func queryLidAction() (int, bool) {
	cmd := exec.Command("powercfg", "/q", "SCHEME_CURRENT", "SUB_BUTTONS")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return -1, false
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "LIDACTION") {
			continue
		}
		// 只要"当前"那一行，避免误读到"可能的设置"列表
		last := -1
		for j := i; j < i+24 && j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			lower := strings.ToLower(t)
			idx := strings.Index(lower, "0x")
			if idx < 0 {
				continue
			}
			hexPart := strings.TrimRight(lower[idx:], " ")
			if len(hexPart) < 10 || hexPart[:9] != "0x0000000" {
				continue
			}
			val := int(hexPart[9] - '0')
			last = val
			if strings.Contains(lower, "current") || strings.Contains(t, "当前") {
				return val, true
			}
		}
		if last >= 0 {
			return last, true
		}
	}
	return -1, false
}

func setLidNoSleep(on bool) bool {
	action := 1 // 关闭时恢复成系统默认的"睡眠"
	if on {
		action = 0
	}
	params := fmt.Sprintf(
		"/c powercfg /setacvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION %d "+
			"&& powercfg /setdcvalueindex SCHEME_CURRENT SUB_BUTTONS LIDACTION %d "+
			"&& powercfg /setactive SCHEME_CURRENT", action, action)
	return runAsAdmin("cmd.exe", params)
}

func lidStatusText() string {
	if !st.lidMode {
		return "合盖即睡眠（系统默认）"
	}
	return "合盖不休眠 · 需通电，注意散热"
}

// 退出前确认：合盖不睡眠是持久化设置，不随程序退出失效
func confirmLidRestore() {
	if !st.lidMode {
		return
	}
	msg := utf16Ptr("合盖不休眠还开着，退出后仍会生效。\n是否恢复为系统默认（合盖睡眠）？")
	title := utf16Ptr("AntiSleep")
	r, _, _ := pMessageBoxW.Call(hwndMain, msg, title, uintptr(4|0x20|0x40000)) // MB_YESNO | MB_ICONQUESTION | MB_TOPMOST
	if r == 6 {                                                        // IDYES
		if setLidNoSleep(false) {
			st.lidMode = false
			saveConfig()
		}
	}
}

func toggleLid() {
	next := !st.lidMode
	if setLidNoSleep(next) {
		st.lidMode = next
		saveConfig()
		if next {
			showToast("已开启合盖不休眠")
		} else {
			showToast("已恢复合盖睡眠")
		}
	} else {
		showToast("已取消，或缺少管理员授权")
	}
	repaint()
}
