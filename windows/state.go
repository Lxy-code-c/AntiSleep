package main

// 状态管理：配置持久化、进程枚举、防息屏执行状态。
// 核心新增：白名单应用守护 —— 指定的应用在运行时，即使屏幕已锁定，
// 也保持系统不休眠，让下载、渲染、编译、备份等任务继续跑完。

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// 版本号由构建时 -ldflags -X main.appVersion=... 注入，避免面板上写死的版本和 tag 对不上。
var appVersion = "1.2.4"

const (
	ES_CONTINUOUS       = 0x80000000
	ES_SYSTEM_REQUIRED  = 0x00000001
	ES_DISPLAY_REQUIRED = 0x00000002
)

// 供界面读取的运行时状态
type runtimeState struct {
	running         bool
	infinite        bool
	totalSec        int
	remainSec       int
	whitelist       []string // 用户添加的进程名（原样保存，通常小写 exe 名）
	activeWhitelist []string // 此刻正在运行的白名单进程（展示用原始大小写）
	autostart       bool
	lidMode         bool
	minutes         int // 输入框里的时长（分钟）
	toast           string
	toastUntil      int64
}

var st = runtimeState{
	minutes: 120,
	remainSec: -1,
	whitelist: nil,
}

func configDir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "AntiSleep")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".antisleep")
}

func configPath() string { return filepath.Join(configDir(), "config.ini") }

// ---- 配置读写 ----

func loadConfig() {
	b, err := os.ReadFile(configPath())
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			k, v, _ := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			switch k {
			case "minutes":
				if n, e := strconv.Atoi(v); e == nil && n >= 1 && n <= 1440 {
					st.minutes = n
				}
			case "lid":
				st.lidMode = v == "1"
			case "whitelist":
				st.whitelist = nil
				for _, item := range strings.Split(v, "|") {
					item = strings.TrimSpace(item)
					if item != "" {
						st.whitelist = append(st.whitelist, item)
					}
				}
			}
		}
	}
	st.totalSec = st.minutes * 60
	st.autostart = queryAutostart()
	if action, ok := queryLidAction(); ok {
		st.lidMode = action == 0
	}
}

func saveConfig() {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return
	}
	var lines []string
	lines = append(lines, "# AntiSleep 配置")
	lines = append(lines, "minutes="+strconv.Itoa(st.minutes))
	lines = append(lines, "whitelist="+strings.Join(st.whitelist, "|"))
	if st.lidMode {
		lines = append(lines, "lid=1")
	} else {
		lines = append(lines, "lid=0")
	}
	_ = os.WriteFile(configPath(), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// ---- 进程枚举 ----

type processEntry32W struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	DwFlags             uint32
	SzExeFile           [260]uint16
}

// 全部进程：小写 exe 名 -> 原始大小写
func allProcesses() map[string]string {
	out := make(map[string]string)
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == uintptr(0xFFFFFFFF) {
		return out
	}
	defer pCloseHandle.Call(snap)

	var pe processEntry32W
	pe.DwSize = uint32(unsafe.Sizeof(pe))
	r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	for r != 0 {
		name := syscall.UTF16ToString(pe.SzExeFile[:])
		if name != "" {
			lower := strings.ToLower(name)
			if _, exists := out[lower]; !exists {
				out[lower] = name
			}
		}
		r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}
	return out
}

// 有可见顶层窗口的进程（添加白名单时给用户选，比较贴近“我在用的应用”）
func windowedProcesses() []string {
	pidSet := make(map[uint32]bool)
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		vis, _, _ := pIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		n, _, _ := pGetWindowTextLengthW.Call(hwnd)
		if n == 0 {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != 0 {
			pidSet[pid] = true
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)

	// pid -> exe 的映射
	name := make(map[uint32]string)
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap != 0 && snap != uintptr(0xFFFFFFFF) {
		var pe processEntry32W
		pe.DwSize = uint32(unsafe.Sizeof(pe))
		r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
		for r != 0 {
			name[pe.Th32ProcessID] = syscall.UTF16ToString(pe.SzExeFile[:])
			r, _, _ = pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
		}
		pCloseHandle.Call(snap)
	}

	self := strings.ToLower(filepath.Base(os.Args[0]))
	var list []string
	seen := make(map[string]bool)
	for pid := range pidSet {
		n := name[pid]
		if n == "" {
			continue
		}
		lower := strings.ToLower(n)
		if lower == self || seen[lower] {
			continue
		}
		// 系统外壳进程没有“后台任务”含义，排除
		switch lower {
		case "explorer.exe", "textinputhost.exe", "applicationframehost.exe",
			"shellexperiencehost.exe", "searchapp.exe", "startmenuexperiencehost.exe",
			"rundll32.exe", "conhost.exe":
			continue
		}
		seen[lower] = true
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i]) < strings.ToLower(list[j]) })
	return list
}

// 当前正在运行的白名单进程
func activeWhitelistProcesses() []string {
	if len(st.whitelist) == 0 {
		return nil
	}
	procs := allProcesses()
	var out []string
	for _, w := range st.whitelist {
		if name, ok := procs[strings.ToLower(strings.TrimSpace(w))]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func addWhitelist(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if !strings.Contains(name, ".") {
		name += ".exe"
	}
	lower := strings.ToLower(filepath.Base(name))
	for _, w := range st.whitelist {
		if strings.ToLower(w) == lower {
			return false
		}
	}
	st.whitelist = append(st.whitelist, lower)
	saveConfig()
	return true
}

func removeWhitelist(name string) {
	lower := strings.ToLower(name)
	for i, w := range st.whitelist {
		if strings.ToLower(w) == lower {
			st.whitelist = append(st.whitelist[:i], st.whitelist[i+1:]...)
			saveConfig()
			return
		}
	}
}

// ---- 防息屏执行 ----

func applyExecutionState() {
	var flags uintptr = ES_CONTINUOUS
	if st.running {
		// 完整防息屏：显示器不关、系统不睡
		flags |= ES_DISPLAY_REQUIRED | ES_SYSTEM_REQUIRED
	} else if len(st.activeWhitelist) > 0 {
		// 仅守护：允许关闭显示器（锁屏后屏幕本来就会关），但系统绝不能休眠，
		// 这样指定应用在锁屏状态下依旧能继续跑后台任务。
		flags |= ES_SYSTEM_REQUIRED
	}
	pSetThreadExecution.Call(flags)
}

func releaseExecutionState() {
	pSetThreadExecution.Call(uintptr(ES_CONTINUOUS))
}

func startRun(infinite bool, minutes int) {
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 1440 {
		minutes = 1440
	}
	st.minutes = minutes
	st.totalSec = minutes * 60
	st.infinite = infinite
	st.running = true
	if infinite {
		st.remainSec = 0
	} else {
		st.remainSec = st.totalSec
	}
	saveConfig()
	applyExecutionState()
	repaint()
}

func stopRun() {
	st.running = false
	st.infinite = false
	st.remainSec = -1
	applyExecutionState()
	repaint()
}

// 每秒执行一次：刷新白名单活跃状态、推进倒计时、申明执行状态
func stepSecond() {
	prevActive := len(st.activeWhitelist)
	st.activeWhitelist = activeWhitelistProcesses()
	wasRunning := st.running
	if st.running && !st.infinite {
		if st.remainSec > 0 {
			st.remainSec--
		}
		if st.remainSec <= 0 {
			st.remainSec = 0
			st.running = false
			if len(st.activeWhitelist) == 0 {
				showToast("时间到，防息屏已停止")
			} else {
				showToast("时间到，转入后台守护")
			}
		}
	}
	applyExecutionState()
	if wasRunning != st.running || prevActive != len(st.activeWhitelist) {
		repaint()
	}
}

func showToast(msg string) {
	st.toast = msg
	st.toastUntil = nowMillis() + 3000
	repaint()
}

func statusText() (string, uint32) {
	if st.running {
		return "防息屏中 · 显示器与系统均保持唤醒", colorGreen
	}
	if len(st.activeWhitelist) > 0 {
		return "后台守护中 · 锁屏后任务继续", colorBlue
	}
	return "未运行", colorDim
}
