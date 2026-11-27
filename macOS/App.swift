import SwiftUI
import AppKit
import Observation
import Foundation
import UniformTypeIdentifiers

// AntiSleep for macOS —— 菜单栏常驻的防息屏工具
//
// v1.1 新增「后台守护应用」：把下载器、渲染、编译、备份等 App 加进守护清单，
// 只要它们在运行，即使屏幕已经锁定，系统也不会进入睡眠，后台任务可以继续跑完。
//
// 主防息屏走 `caffeinate -d -i -m -s -u`（显示器、系统、磁盘、用户空闲全挡）；
// 单纯守护走 `caffeinate -i -w <pid>`（只挡系统睡眠，允许关屏/锁屏）。

// MARK: - 模型

struct WatchApp: Codable, Identifiable, Hashable {
    var id: String
    var name: String
    var bundleID: String
    var path: String
}

struct Candidate: Identifiable, Hashable {
    var id: String
    var name: String
    var bundleID: String
    var path: String
}

final class AntiSleepModel: ObservableObject {
    static let shared = AntiSleepModel()

    // 主防息屏
    @Published var isRunning = false
    @Published var infinite = false
    @Published var remaining = 0
    @Published var minutes: Int = 120 {
        didSet { if !isRunning { save() } }
    }
    @Published var toast: String = ""
    @Published var toastUntil: Date = .distantPast

    // 后台守护
    @Published var watchList: [WatchApp] = []
    @Published private(set) var activeIDs: Set<String> = []
    @Published private(set) var activePIDs: [String: pid_t] = [:]
    @Published private(set) var candidates: [Candidate] = []

    // 开机自启
    @Published var autostart: Bool = false
    // 合盖不睡眠（pmset disablesleep，需要管理员授权）
    @Published var lidMode: Bool = false

    private var mainProc: Process?
    private var totalSeconds = 0
    private var startDate: Date?
    private var guardProcs: [String: Process] = [:]
    private var ticker: Timer?
    private var activityToken: NSObjectProtocol?

    private let defaults = UserDefaults.standard
    private let whitelistKey = "AntiSleep.watchList"
    private let minutesKey = "AntiSleep.minutes"

    init() {
        load()
        autostart = LaunchAgent.isEnabled()
        lidMode = LidSleep.isDisabled
        refreshCandidates()
        refreshActive()
        LaunchAgent.refreshIfEnabled()
        DispatchQueue.main.async { [weak self] in
            self?.startTicker()
        }
    }

    // MARK: 展示文案

    var statusText: String {
        if isRunning {
            return infinite ? "持续防息屏中" : "防息屏中 · 屏幕不会锁、系统不会睡"
        }
        if !activeIDs.isEmpty {
            return "后台守护中 · \(activeIDs.count) 个应用在运行，锁屏也继续"
        }
        return "未运行"
    }

    var statusKind: Int {
        if isRunning { return 1 }
        if !activeIDs.isEmpty { return 2 }
        return 0
    }

    var bigText: String {
        if isRunning {
            return infinite ? "∞" : Self.clock(remaining)
        }
        if !activeIDs.isEmpty { return "守 护" }
        return "--:--"
    }

    var subText: String {
        if isRunning && infinite { return "一直防息屏，直到手动停止" }
        if isRunning { return "剩余时间 · 到时自动停止" }
        if !activeIDs.isEmpty { return "指定应用还在跑，已阻止系统睡眠" }
        return "设置时长后开始，或先添加守护应用"
    }

    var menuSymbol: String {
        if isRunning { return "shield.fill" }
        if !activeIDs.isEmpty { return "shield.lefthalf.filled" }
        return "shield"
    }

    static func clock(_ sec: Int) -> String {
        let s = max(0, sec)
        let h = s / 3600, m = (s % 3600) / 60, x = s % 60
        if h > 0 { return String(format: "%02d:%02d:%02d", h, m, x) }
        return String(format: "%02d:%02d", m, x)
    }

    // MARK: 主防息屏

    func start(minutes: Int) {
        let clamped = min(max(minutes, 1), 1440)
        self.minutes = clamped
        save()
        totalSeconds = clamped * 60
        remaining = totalSeconds
        infinite = false
        startDate = Date()
        launchMain(seconds: totalSeconds)
        isRunning = true
        beginActivity()
        show("防息屏已开始：\(clamped) 分钟")
    }

    func startInfinite() {
        totalSeconds = 0
        remaining = 0
        infinite = true
        isRunning = true
        startDate = nil
        launchMain(seconds: nil)
        beginActivity()
        show("已开始持续防息屏")
    }

    func stop() {
        stopMainProc()
        isRunning = false
        infinite = false
        remaining = 0
        startDate = nil
        endActivity()
        show("已停止防息屏")
    }

    private func launchMain(seconds: Int?) {
        stopMainProc()
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/caffeinate")
        var args = ["-d", "-i", "-m", "-s", "-u"]
        if let s = seconds { args.append(contentsOf: ["-t", String(s)]) }
        p.arguments = args
        p.terminationHandler = { proc in
            DispatchQueue.main.async {
                // 只有带 -t 的定时进程自然结束才收尾；手动终止由 stop() 处理
                guard seconds != nil else { return }
                if proc.terminationStatus == 0 && AntiSleepModel.shared.isRunning {
                    AntiSleepModel.shared.isRunning = false
                    AntiSleepModel.shared.remaining = 0
                    AntiSleepModel.shared.endActivity()
                    AntiSleepModel.shared.show("时间到，防息屏已停止")
                }
            }
        }
        do {
            try p.run()
            mainProc = p
        } catch {
            isRunning = false
            show("启动 caffeinate 失败")
        }
    }

    private func stopMainProc() {
        if let p = mainProc, p.isRunning {
            p.terminate()
        }
        mainProc = nil
    }

    // 让本应用自身在后台不被 App Nap 挂起，保证倒计时和守护检测准确
    private func beginActivity() {
        guard activityToken == nil else { return }
        activityToken = ProcessInfo.processInfo.beginActivity(
            options: [.userInitiated, .idleSystemSleepDisabled],
            reason: "AntiSleep 防息屏与后台守护"
        )
    }

    private func endActivity() {
        if let token = activityToken {
            ProcessInfo.processInfo.endActivity(token)
            activityToken = nil
        }
    }

    // MARK: 后台守护

    func add(_ c: Candidate) {
        if watchList.contains(where: { $0.bundleID == c.bundleID && c.bundleID != "" }) { return }
        if watchList.contains(where: { $0.path == c.path && c.path != "" }) { return }
        var id = c.bundleID.isEmpty ? c.path : c.bundleID
        if id.isEmpty { id = c.name }
        watchList.append(WatchApp(id: id, name: c.name, bundleID: c.bundleID, path: c.path))
        save()
        refreshCandidates()
        refreshActive()
        syncGuard()
        show("已加入守护：\(c.name)")
    }

    func remove(_ app: WatchApp) {
        watchList.removeAll { $0.id == app.id }
        if let p = guardProcs[app.id] {
            p.terminate()
            guardProcs.removeValue(forKey: app.id)
        }
        save()
        refreshCandidates()
        refreshActive()
        show("已移除守护：\(app.name)")
    }

    func refreshCandidates() {
        let apps = NSWorkspace.shared.runningApplications
        var seen = Set<String>()
        var out: [Candidate] = []
        let selfID = Bundle.main.bundleIdentifier ?? ""
        for app in apps {
            guard app.activationPolicy == .regular, !app.isTerminated else { continue }
            guard let bid = app.bundleIdentifier, bid != selfID else { continue }
            if watchList.contains(where: { $0.bundleID == bid }) { continue }
            if seen.contains(bid) { continue }
            seen.insert(bid)
            out.append(Candidate(
                id: bid,
                name: app.localizedName ?? bid,
                bundleID: bid,
                path: app.bundleURL?.path ?? ""
            ))
        }
        candidates = out.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    func pickAppFromDisk() {
        NSApp.activate(ignoringOtherApps: true)
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        panel.allowedContentTypes = [.application]
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        panel.message = "选择一个 .app 加入后台守护"
        panel.prompt = "加入守护"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        if let bundle = Bundle(url: url), let bid = bundle.bundleIdentifier {
            let name = bundle.infoDictionary?["CFBundleName"] as? String
                ?? url.deletingPathExtension().lastPathComponent
            add(Candidate(id: bid, name: name, bundleID: bid, path: url.path))
        } else {
            add(Candidate(id: url.path, name: url.deletingPathExtension().lastPathComponent,
                          bundleID: "", path: url.path))
        }
    }

    func refreshActive() {
        let apps = NSWorkspace.shared.runningApplications
        var ids = Set<String>()
        var pids: [String: pid_t] = [:]
        for w in watchList {
            let match = apps.first { app in
                if app.isTerminated { return false }
                if !w.bundleID.isEmpty && app.bundleIdentifier == w.bundleID { return true }
                if !w.path.isEmpty && app.bundleURL?.path == w.path { return true }
                return false
            }
            if let app = match, app.processIdentifier > 0 {
                ids.insert(w.id)
                pids[w.id] = app.processIdentifier
            }
        }
        activeIDs = ids
        activePIDs = pids
    }

    func syncGuard() {
        // 清理已退出的守护进程（先收集再删除，避免边遍历边改字典）
        let dead = guardProcs.filter { !$0.value.isRunning }.map { $0.key }
        for id in dead {
            guardProcs.removeValue(forKey: id)
        }
        // 应用已退出 → 停掉对应 caffeinate
        let gone = guardProcs.filter { activePIDs[$0.key] == nil }.map { $0.key }
        for id in gone {
            if let p = guardProcs[id], p.isRunning { p.terminate() }
            guardProcs.removeValue(forKey: id)
        }
        // 应用在跑但没有守护进程 → 拉起 `caffeinate -i -w <pid>`
        for (id, pid) in activePIDs where guardProcs[id] == nil {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: "/usr/bin/caffeinate")
            p.arguments = ["-i", "-w", String(pid)]
            do {
                try p.run()
                guardProcs[id] = p
            } catch {
                // 静默失败，下一轮重试
            }
        }
    }

    // MARK: 计时

    private func startTicker() {
        ticker?.invalidate()
        ticker = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
            self?.tick()
        }
        if let t = ticker {
            RunLoop.main.add(t, forMode: .common)
        }
    }

    private func tick() {
        if isRunning, !infinite, let start = startDate {
            let elapsed = Int(Date().timeIntervalSince(start))
            remaining = max(0, totalSeconds - elapsed)
            if remaining == 0 && mainProc?.isRunning != true {
                isRunning = false
                endActivity()
            }
        }
        refreshActive()
        syncGuard()
        if toastUntil < Date() { toast = "" }
    }

    func show(_ message: String) {
        toast = message
        toastUntil = Date().addingTimeInterval(3)
    }

    // MARK: 退出清理

    func shutdown() {
        stop()
        for (_, p) in guardProcs { p.terminate() }
        guardProcs.removeAll()
        ticker?.invalidate()
        ticker = nil
    }

    // MARK: 持久化

    private func load() {
        if let data = defaults.data(forKey: whitelistKey),
           let list = try? JSONDecoder().decode([WatchApp].self, from: data) {
            watchList = list
        }
        let m = defaults.integer(forKey: minutesKey)
        if m >= 1 && m <= 1440 { minutes = m }
    }

    private func save() {
        defaults.set(minutes, forKey: minutesKey)
        if let data = try? JSONEncoder().encode(watchList) {
            defaults.set(data, forKey: whitelistKey)
        }
    }
}

// MARK: - 开机自启（LaunchAgent）

enum LaunchAgent {
    static let label = "com.lxy.antisleep"

    static var plistURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/\(label).plist")
    }

    static var appPath: String {
        let bundle = Bundle.main.bundlePath
        if bundle.hasSuffix(".app") {
            return bundle + "/Contents/MacOS/AntiSleep"
        }
        return CommandLine.arguments[0]
    }

    static func isEnabled() -> Bool {
        FileManager.default.fileExists(atPath: plistURL.path)
    }

    @discardableResult
    static func setEnabled(_ on: Bool) -> Bool {
        if on {
            let dir = plistURL.deletingLastPathComponent()
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let plist: [String: Any] = [
                "Label": label,
                "ProgramArguments": [appPath],
                "RunAtLoad": true,
                "ProcessType": "Interactive"
            ]
            guard let data = try? PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0) else {
                return false
            }
            do {
                try data.write(to: plistURL)
                // 不用 unload/load：unload 会终止由该 login item 启动的本进程，
                // 表现就是用户一取消勾选，App 自己消失了。
                let uid = String(getuid())
                run(["launchctl", "enable", "gui/\(uid)/\(label)"])
                run(["launchctl", "bootstrap", "gui/\(uid)", plistURL.path])
                return true
            } catch {
                return false
            }
        } else {
            let uid = String(getuid())
            run(["launchctl", "disable", "gui/\(uid)/\(label)"])
            try? FileManager.default.removeItem(at: plistURL)
            return true
        }
    }

    /// App 被拖到别的目录后，plist 里的旧路径会让登录不启动；启动时顺手重写。
    static func refreshIfEnabled() {
        guard isEnabled() else { return }
        _ = setEnabled(true)
    }

    private static func run(_ args: [String]) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: args[0])
        p.arguments = Array(args.dropFirst())
        try? p.run()
        p.waitUntilExit()
    }
}

// MARK: - 合盖不睡眠（pmset disablesleep）

enum LidSleep {
    static var isDisabled: Bool {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/pmset")
        p.arguments = ["-g"]
        let pipe = Pipe()
        p.standardOutput = pipe
        do { try p.run() } catch { return false }
        p.waitUntilExit()
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        guard let text = String(data: data, encoding: .utf8) else { return false }
        for line in text.split(separator: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("disablesleep") {
                return t.split(separator: " ").last.map { $0 == "1" } ?? false
            }
        }
        return false
    }

    /// 需要管理员密码（系统弹窗），关闭时恢复系统默认 0
    static func set(disabled: Bool) -> Bool {
        let script = "do shell script \"/usr/bin/pmset -a disablesleep \(disabled ? 1 : 0)\" with administrator privileges"
        guard let apple = NSAppleScript(source: script) else { return false }
        var err: NSDictionary?
        apple.executeAndReturnError(&err)
        return err == nil
    }
}

// MARK: - 界面

struct AntiSleepView: View {
    @ObservedObject private var model = AntiSleepModel.shared
    @State private var minutesText: String = ""
    @State private var showQuitConfirm = false
    @FocusState private var minutesFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            statusRow
            bigClock
            durationRow
            actionRow
            Divider().opacity(0.35)
            guardSection
            footer
        }
        .padding(14)
        .frame(width: 340)
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear {
            minutesText = String(model.minutes)
            model.refreshCandidates()
        }
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            Text("AntiSleep").font(.system(size: 20, weight: .semibold))
            Spacer()
            Button("退出") {
                if model.lidMode {
                    showQuitConfirm = true
                } else {
                    NSApp.terminate(nil)
                }
            }
            .buttonStyle(.plain)
            .font(.caption)
            .foregroundStyle(.secondary)
            .alert("合盖不休眠还开着", isPresented: $showQuitConfirm) {
                Button("恢复合盖睡眠并退出", role: .destructive) {
                    _ = LidSleep.set(disabled: false)
                    model.lidMode = false
                    NSApp.terminate(nil)
                }
                Button("保持设置并退出") { NSApp.terminate(nil) }
                Button("取消", role: .cancel) {}
            } message: {
                Text("这个设置会一直保留到手动关闭。合盖放在包里时机器不会降温，容易过热。")
            }
        }
    }

    private var statusRow: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(statusColor)
                .frame(width: 7, height: 7)
            Text(model.statusText)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }

    private var statusColor: Color {
        switch model.statusKind {
        case 1: return .green
        case 2: return .blue
        default: return .secondary
        }
    }

    private var bigClock: some View {
        VStack(spacing: 2) {
            Text(model.bigText)
                .font(.system(size: 34, weight: .bold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(model.isRunning ? Color.green : (model.activeIDs.isEmpty ? Color.secondary : Color.blue))
                .frame(maxWidth: .infinity)
            Text(model.subText)
                .font(.system(size: 11))
                .foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity)
        }
    }

    private var durationRow: some View {
        HStack(spacing: 6) {
            TextField("分钟", text: $minutesText)
                .textFieldStyle(.roundedBorder)
                .multilineTextAlignment(.center)
                .frame(width: 58)
                .focused($minutesFocused)
                .onSubmit { applyMinutes() }
            Text("分钟").font(.system(size: 11)).foregroundStyle(.secondary)
            Spacer(minLength: 4)
            ForEach([30, 60, 120], id: \.self) { m in
                chip("\(m)") {
                    minutesText = "\(m)"
                    model.minutes = m
                }
            }
            chip("∞") { model.startInfinite() }
        }
        .disabled(model.isRunning)
    }

    private func chip(_ label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: 11, weight: .medium))
                .frame(width: 34, height: 24)
                .background(RoundedRectangle(cornerRadius: 7).fill(Color.secondary.opacity(0.14)))
        }
        .buttonStyle(.plain)
    }

    private var actionRow: some View {
        HStack(spacing: 8) {
            if model.isRunning {
                Button {
                    model.stop()
                } label: {
                    Text("停 止")
                        .font(.system(size: 13, weight: .semibold))
                        .frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.borderedProminent)
                .tint(.red)
            } else {
                Button {
                    applyMinutes()
                    model.start(minutes: model.minutes)
                } label: {
                    Text("开始防息屏")
                        .font(.system(size: 13, weight: .semibold))
                        .frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.borderedProminent)
                .tint(.blue)
            }
        }
    }

    private var guardSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("后台守护应用").font(.system(size: 13, weight: .semibold))
                Spacer()
                Menu {
                    if model.candidates.isEmpty {
                        Text("没有找到可添加的应用")
                    } else {
                        ForEach(model.candidates) { c in
                            Button(c.name) { model.add(c) }
                        }
                    }
                    Divider()
                    Button("选择 .app…") { model.pickAppFromDisk() }
                    Button("刷新列表") { model.refreshCandidates() }
                } label: {
                    Label("添加", systemImage: "plus")
                        .font(.system(size: 11))
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
            }

            Text("这些应用运行时，锁屏也不休眠")
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)

            if model.watchList.isEmpty {
                Text("还没有守护应用")
                    .font(.system(size: 11))
                    .foregroundStyle(.tertiary)
                    .padding(.vertical, 4)
            } else {
                ScrollView {
                    VStack(spacing: 4) {
                        ForEach(model.watchList) { app in
                            HStack(spacing: 8) {
                                Circle()
                                    .fill(model.activeIDs.contains(app.id) ? Color.green : Color.secondary.opacity(0.4))
                                    .frame(width: 6, height: 6)
                                Text(app.name)
                                    .font(.system(size: 11))
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                                Spacer()
                                Button {
                                    model.remove(app)
                                } label: {
                                    Image(systemName: "xmark")
                                        .font(.system(size: 9, weight: .semibold))
                                        .foregroundStyle(.tertiary)
                                }
                                .buttonStyle(.plain)
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(RoundedRectangle(cornerRadius: 7).fill(Color.secondary.opacity(0.08)))
                        }
                    }
                }
                .frame(maxHeight: 150)
            }
        }
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: 6) {
            Toggle(isOn: Binding(
                get: { model.autostart },
                set: { newValue in
                    model.autostart = LaunchAgent.setEnabled(newValue) ? newValue : model.autostart
                }
            )) {
                Text("开机自启").font(.system(size: 11))
            }
            .toggleStyle(.checkbox)

            Toggle(isOn: Binding(
                get: { model.lidMode },
                set: { newValue in
                    if LidSleep.set(disabled: newValue) {
                        model.lidMode = newValue
                        model.show(newValue ? "已开启合盖不休眠（注意散热）" : "已恢复合盖睡眠")
                    } else {
                        model.show("已取消管理员授权，未修改设置")
                    }
                }
            )) {
                Text("合盖也不休眠（需管理员授权）").font(.system(size: 11))
            }
            .toggleStyle(.checkbox)

            if !model.toast.isEmpty {
                Text(model.toast)
                    .font(.system(size: 10))
                    .foregroundStyle(.blue)
            }
            Text(model.lidMode ? "合盖不休眠已开启：注意放在通风处、接电源" : "关机/睡眠仍需手动；守护只阻止空闲睡眠")
                .font(.system(size: 9))
                .foregroundStyle(.quaternary)
        }
    }

    private func applyMinutes() {
        if let v = Int(minutesText.trimmingCharacters(in: .whitespaces)), v >= 1, v <= 1440 {
            model.minutes = v
        } else {
            minutesText = String(model.minutes)
        }
    }
}

// MARK: - 入口

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationWillTerminate(_ notification: Notification) {
        AntiSleepModel.shared.shutdown()
    }
}

@main
struct AntiSleepApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @ObservedObject private var model = AntiSleepModel.shared

    var body: some Scene {
        MenuBarExtra {
            AntiSleepView()
        } label: {
            Image(systemName: model.menuSymbol)
        }
        .menuBarExtraStyle(.window)
    }
}
