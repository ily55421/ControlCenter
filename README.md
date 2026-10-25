# Control Center (Go 重构版)

Clevo/Insyde **Control Center 3.0 Package v7.022** 的完整 Go 语言还原与重构。

逆向来源:
- 安装包:`C:\soft\04_ControlCenter_7.022\04_ControlCenter`(InstallShield,data2.cab 221MB)
- 运行目录:`C:\Program Files (x86)\ControlCenter`

逆向手段:.NET 反编译(ILSpy,主程序/服务/全部功能应用/工具)+ 原生二进制反汇编
(radare2,还原 InsydeDCHU.dll 的 ACPI 桥驱动 IOCTL 协议)+ 事件日志/注册表/配置文件分析。

**协议已在真实硬件上验证**(COLORFUL P16 Pro IXA1 / Clevo ID 25990):
ACPI 桥设备枚举、4KB 设置区读写、WMI 16/96/122/12/13/17 整数与包命令、WMI4 多路复用包
(键盘设置查询)全部按原协议工作。

## 与原组件的对应关系

| 原组件 | Go 组件 | 说明 |
|---|---|---|
| AcpiBridge.sys / AcpiBridge1.sys | **沿用原驱动** | 内核驱动不可用 Go 重写;用户态协议已完整还原 |
| InsydeDCHU.dll | `internal/acpibridge` | IOCTL 0x322400/04/08/0C 客户端(6 个导出函数等价) |
| DCHUService.exe | `cmd/ccservice` | 热键事件源 + w_arg253/254/255 进程通道 + 会话管理 |
| FnKey.exe | `cmd/ccfnkey` | 托盘/OSD/电源模式引擎/热键分发 |
| ControlCenter30.exe | `cmd/ccapp` | 主界面(磁贴/模式/偏好设置) |
| FanSpeedSetting / Keyboard / Flexikey / BatteryUtility / EnergySave / CPU_OC / GPUOverclocking appx | `cmd/ccfan` 等 + `internal/gui` | 全部功能面板(**原生 GUI**,Gio/D3D11 硬件渲染) |
| CClnk / RunAutoInstall / RemoveAPP / clean / WakePanelUp_Hook | `cmd/ccutil` 子命令 | 工具集 |

## 构建

```bash
go build ./...                # 全部包
go test ./internal/...        # 协议编码单元测试
for t in ccapp ccservice ccfnkey ccfan cckeyboard ccflexikey ccbattery ccenergy ccpuoc ccgpuoc ccutil; do
  go build -o bin/$t.exe ./cmd/$t
done
```

依赖 `golang.org/x/sys` 与 `gioui.org`(原生 GUI,Windows 上纯 Go 直调 D3D11,无需 cgo),其余全部标准库。仅支持 Windows x64。

## 部署与运行(全 GUI,无脚本)

前置:原版 AcpiBridge 驱动已安装(设备 `ACPI\CLV0001` 存在,通常由原版包安装一次)。

**双击 `ControlCenter.exe` 打开启动器**,按钮操作:

| 按钮 | 动作 |
|---|---|
| ① 一键安装 | 自动请求 UAC 提权 → 创建事件日志 → 安装并启动 CCDCHUService 服务 → 创建桌面快捷方式 → 拉起托盘与主界面 |
| ② 启动 Control Center | 免管理员:启动托盘(ccfnkey)与主界面(ccapp **原生窗口**) |
| 硬件诊断 | ACPI 桥连接 / 设置区 / WMI 命令 / 风扇实时 / 键盘灯 / SMBIOS 全量检测,结果显示在窗口日志区 |
| 卸载 | 自动提权 → 停止并移除服务 → 结束后台进程 |

命令行(可选):`ControlCenter.exe -install` / `-uninstall` / `-selftest`(与按钮等价,便于自动化)。
窗口内日志区实时显示操作结果;未提权时自动弹出 UAC,**无需任何 bat 脚本**。

## GUI 界面

功能面板是**真正的 Windows 原生 GUI**(`internal/gui`,Gio 即时模式 + Direct3D11 硬件渲染,
纯 Go 无 cgo):仪表盘/色环/键帽图/功耗曲线全部矢量自绘,静态零 GPU 负载、数据变化才重绘;
后端直连 internal 引擎层(无 HTTP/JSON 序列化),轮询按当前页择要采样(系统页 2s、节能页 1s),
慢速 EC 读不再阻塞其他面板。视觉对齐原 Web UI 的 CC40 霓虹风格(微软雅黑确定性渲染)。

| 入口 | 行为 |
|---|---|
| `ccapp.exe`(主界面) | 单实例互斥量 → 原生窗口(标题 `Control Center`)→ 常驻 |
| `ccfnkey.exe`(托盘) | 托盘图标 + 5 种电源模式菜单 + 点击打开主窗口;随会话常驻 |
| `ccfan/cckeyboard/ccgpuoc/ccbattery/ccenergy/ccpuoc/ccflexikey` | 经 `launcher` 激活运行中的 ccapp 并**切到对应页**;未运行则以 `-page=<页>` 拉起 |
| 托盘/热键"打开面板" | 原版 UWP 已安装则走原版通道(互操作);未安装则打开本项目对应面板 |

面板深链:`ccapp.exe -page=<page>` / 跨进程 `WM_COPYDATA` 导航(message-only 窗口
`ControlCenterGo-Nav`),`page` 支持
`system / kb / flexikey / charger / cpu / gpu / bat / energy / pref`,以及原版别名
(`keyboard`、`perkey`、`fan`、`power`、`gpuoc`、`cpuoc`、`battery`、`flexicharger`、`preferences` 等)。

退出:关闭窗口(点 X)即清运行标志并退出 ccapp;托盘菜单"退出"关闭托盘;托盘/热键的
关闭命令(`clevofnkey^204`)经事件总线送达 ccapp 优雅退出。

## 架构

```
┌─ ccservice(系统服务)────────────────────────────┐
│  • EventLoop:IOCTL 0x322404/08 → Arg001^N 广播  │
│  • 命令通道:w_arg253/254/255 → 以用户身份执行    │
│  • 会话登录/解锁 → 拉起 ccfnkey                  │
└───────────────────────┬──────────────────────────┘
                        │ 事件日志(OutLog)
┌─ ccfnkey(托盘)──────▼──────────────────────────┐
│  • 电源模式引擎(WMI 121/25 + 电源计划 + 风扇/GPU │
│    联动 + clevofnkey^202 广播)                   │
│  • 热键分发(50+ 事件:键盘灯/触摸板/摄像头/飞行…)│
│  • OSD(原生 Win32 分层窗口)                     │
└───────────────────────┬──────────────────────────┘
┌─ ccapp + internal/gui ▼(原生 GUI,D3D11)────────┐
│  电源模式 │ 偏好设置 │ 风扇 │ 键盘灯 │ CPU/GPU 超频│
│  电池 │ 节能计划 │ Flexikey(ccflexikey + 驱动)  │
└──────────────────────────────────────────────────┘
```

## 还原的协议(核心知识产权)

详见 `docs/`:

- **01-ACPI桥驱动协议.md** — IOCTL 0x322400 的 `_DSM` 求值结构(GUID 93F224E4-…/0x104/0x01000002)、
  输出条目格式(type 0 整数 / type 2 缓冲,步长 4+max(4,size))、0x32240C 的 4KB 设置区读
  ({0,0x10000000,0} → 0x1000)/写({1,addr16,len16,data})、事件等待对(0x322404/08)
- **02-事件日志IPC与热键事件.md** — OutLog/PowerBiosServerLog 双通道、全部 `w_arg`/`clevocc30^`/`clevofnkey^` 消息、
  Arg001 热键事件号全表(50+ 事件的完整动作)
- **03-电源模式引擎.md** — ChangePowerMode 全序列(SetPowerMode 编码、NV 上下电、GPU OC 联动、
  EnergyStar、ASPM、Windows 电源计划 GUID、AMD PPT/TDC/EDC、风扇联动、XTU 触发)
- **00-架构与组件映射.md** — 组件映射与 AppSettings 4KB 布局总表(page 0-7 全部偏移含义)
- **功能对比.html** — 原版 vs Go 版功能点矩阵/组件完成度/真机验证结果(单文件可视化页,浏览器直接打开;数据内联在 `<script>` 的 `COMPONENTS/MATRIX/TESTS` 常量中,改数即改页)

## 关键实现

| 模块 | 文件 | 要点 |
|---|---|---|
| 桥驱动客户端 | `internal/acpibridge` | 输入结构逐字节构造;条目解析与原生 DLL 前进算法一致 |
| 事件日志 IPC | `internal/eventbus` | RegisterEventSource/ReadEventLog(NotifyChangeEventLog),插入字符串与 .NET 互读 |
| WMI 命令 | `internal/wmi` | SetWMI 子命令编码(data 低 3 字节 + sub 在 byte[3])、WMI4 多路复用、EC RAM 直访 |
| 电源模式 | `internal/powermode` | 完整切换序列 + 模式循环链 |
| 风扇 | `internal/fan` | WMI 12 实时(RPM=2156160/raw)、121 子命令、WMI14 三点曲线(斜率×2.55×16)、防尘 |
| 键盘灯 | `internal/keyboard` | WMI 39 白光 / 103 RGB(0xE0 位图、0xF4 亮度、分区色 0xF0-0xF6、模式常量)、AppSettings page2 镜像 |
| 电池 | `internal/battery` | WMI7 包解析、健康判定(替换 VBScript 表达式求值)、校准状态机 |
| 节能 | `internal/energysave` | WMI 65 ADC 功耗采样(字节交换/倍率/19.2V)、118 充放电调度编码 |
| CPU 超频 | `internal/cpuoc` | page6 全参数表 + Apply/Default 逐条写序列 |
| GPU 超频 | `internal/gpu` | page5 非对称 int16 编码(写 n-1 / 读 raw-65535)+ ControlGPU 通道 |
| Flexikey | `internal/flexikey` | kb.dll 全部导出 syscall 绑定(三重注册 57463/57464)、profile.xml 兼容读写 |
| OSD/托盘 | `internal/win32` | Shell_NotifyIcon、分层窗口 + GDI、WM_COPYDATA、电源 API、WTS 会话 |

## 说明与边界

1. **内核驱动不重写**:AcpiBridge.sys/AcpiBridge1.sys/HKFltr 驱动沿用原版(Go 无法编写内核驱动);
   Go 代码以还原的 IOCTL 协议直接通信,不再依赖 InsydeDCHU.dll。
2. **GPU OC 的 NVAPI 落地**:与原版一致,经 `ControlGPU.exe set_increment` 通道(原版亦如此);偏移
   持久化(AppSettings page5)与编码逐字节一致。
3. **CPU 超频的 XTU 执行**:与原版一致,参数写入 AppSettings page6 由 BIOS/EC/XTU 后台执行;
   Go 侧完成全部参数读写与 Apply/Default 序列。
4. **UI**:功能面板为原生 Gio/D3D11 GUI(风扇曲线/RGB 调色/超频滑杆等富交互,直连引擎层);托盘与 OSD 为原生 Win32
   (syscall 直调,无 CGO)。
5. **事件日志兼容**:与原版 .NET 组件互发消息时使用相同的 Source/Log 与消息格式;若系统仍装着原版
   组件,二者可通过同一事件总线互通。
