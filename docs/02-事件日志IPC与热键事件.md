# 事件日志 IPC 与热键事件(逆向还原)

## 通道

| Log | Source | 方向 | 用途 |
|---|---|---|---|
| OutLog | PowerBIOSServer_Out | 双向广播 | 组件间事件总线(`clevocc30^…`、`clevofnkey^…`、`Arg001^…`、`clevofan^…` 等) |
| PowerBiosServerLog | PowerBiosServerSource | UI/应用 → 服务 | 命令通道(`w_arg253/254/255^…` 请求服务执行进程) |

写:`RegisterEventSource` + `ReportEvent`(EVENTLOG_INFORMATION_TYPE)。
订阅:`OpenEventLog` + `NotifyChangeEventLog` + `ReadEventLog` 轮询(从最新记录开始)。
前提:两个日志均须存在(EventCreate / wevtutil 创建),否则消息静默丢弃。

## 消息协议

前缀 `^` 分段,第一段为发送者,第二段为命令号,后续为参数。

### UI/应用 → 服务(PowerBiosServerLog)

| 消息 | 语义 |
|---|---|
| `w_arg253^launchapp^<PFN>^yes^<进程名>` | 启动 UWP 应用(explorer shell:appsFolder,可杀其他会话实例) |
| `w_arg253^<exe路径>` | 启动普通程序(runas) |
| `w_arg254^<命令行>` | 以控制台用户身份执行命令(绕过 UAC,复制 explorer 令牌) |
| `w_arg255^<exe>^<参数>` | 带参执行 |
| `clevofnkey^1ff` | Fn+F2 触摸板 hook(服务转给 WakePanelUp_Hook.exe) |
| `cpuoc_bg_10a/10b/10c/107/108` | XTU 后台命令(服务转给 CPUOC\CC30_BG.exe) |
| `Close_Loading` | 通知 loading 宿主关闭 |
| `Exit Windows` | 退出 |

### OutLog 广播事件

| 消息 | 语义 |
|---|---|
| `Arg001^<value>` | 热键事件(value&0xFF=事件号,value>>8=数据) |
| `clevofnkey^102` | 语言切换 → 全体重载多语言 |
| `clevofnkey^201` | 键盘灯亮度变化 |
| `clevofnkey^202` / `Set PowerMode:` | 电源模式已切换 → 重读 AppSettings(1,1) |
| `clevofnkey^203` | 关闭键盘灯应用 |
| `clevofnkey^204` | 关闭 CC 主界面 |
| `clevofnkey^205` | Flexikey 开关状态变化 |
| `clevofnkey^206^0/1` | 触摸板关/开 |
| `clevofnkey^20b^0/1` | 电源模式 UI 解锁/锁定 |
| `clevofnkey^20d` | 触摸板事件 |
| `clevofnkey^20e` | FnLock 切换 |
| `clevofnkey^209` | RGB 换色 |
| `clevofnkey^20A` | 键盘灯开关 |
| `clevofnkey^210` | PD 功率等级 |
| `clevocc30^101^0..4` | 请求切换电源模式(FnKey 执行) |
| `clevocc30^10f^0/1` | AMD 手动模式退出/进入 |
| `clevocc30^112^0/1` | Turbo 风扇关/开 |
| `clevocc30^114` | 低刷新率设置已写入 |
| `clevofan^113^<mode>` | 风扇模式变化 |
| `clevobattery^207^0/1` | 电池校准结束/开始 |
| `dchu^10e/110/111` | 服务要求 FnKey 关闭/Arg001 状态查询 |
| `flexikey^115^1/2` | OSFD 文件打开/保存对话 |
| `osfd^1/2^<file>` | OSFD 结果 |
| `clevokeyboard^202^<mode>` | 键盘灯效切换 |

## 热键事件号(Arg001 低字节)处理表(FnKey 实现)

| 事件号 | 含义 | 动作摘要 |
|---|---|---|
| 32/33 | 键盘灯 -/+ | KB.SetBrightnessDown/Up;OSD;广播 201 |
| 37 | FnLock 开关 | WMI4 sub24 读 → sub25 写(1/2);OSD;广播 20e |
| 38 | 麦克风静音 | CoreAudio 全端点 Mute/Unmute;写 (1,41);OSD |
| 44/45 | 低电量 1/2 | WMI4 sub46 停轮询;按 DC 电池动作(S3/S4/关机) |
| 46 | PD 功率 | WMI4 sub55 [1]=1;广播 210 |
| 47 | 电池温度 | EC RAM 223 读温度;<11℃ 且 <81% 警告 |
| 48-56,78/79 | 亮度档 | WMI17[210..220] 目标亮度;PowerWriteAC/DCValueIndex+SetActiveScheme |
| 57 | RGB 换色 | KB.Change_ToggleRGBColor;广播 209 |
| 58 | 风扇 Turbo OSD | OSD |
| 59/60 | 电池低/恢复 | SetWMI(121,44,2/3);记录/恢复亮度 |
| 63 | 键盘灯总开关 | 翻转 (2,80);SetStatus;OSD;广播 20A |
| 64 | 除尘运行 | OSD |
| 65-71 | AMD PROC_HOT/功耗限制 | pcc.exe PPT/TDC/EDC;GPU Turbo 联动 |
| 72 | 屏灭关机 | MySystemShutDown |
| 84 | NVSR/屏灭 | cc30wk 0x0025 2;OLED → SetWMI(94,1) |
| 89 | 电源模式循环 | NextMode;OSD;ChangePowerMode;3 秒忽略窗口 |
| 93 | TP 状态 | TPStatus.exe |
| 96/97 | GPU Turbo off/on | 限频/恢复 |
| 106/107/112/143 | 风扇 MaxQ/Custom/Auto/Max | OSD + Fn1_ChangeFanMode(5/6/0/1) |
| 123 | FlexiKey 开关 | 翻转 (1,8)(1,9);FlexikeyInit;广播 205 |
| 124 | 屏幕旋转 180° | ChangeDisplaySettingsEx |
| 129/130 | 键盘灯 -/+(RGB) | 同 32/33 |
| 131 | 打开键盘灯设置 | clevokeyboard: URI / CC40 |
| 132 | 恢复界面 | control.exe /name Microsoft.Recovery |
| 133 | 飞行模式 | SetWMI(121,26,1/0)+SetAirplaneMode |
| 138 | 白光键盘循环 | GetWMI(61) 回读循环 0-4 |
| 139/140/141 | RGB 左/中/右区开关 | 翻转分区状态 |
| 149 | 打开/关闭 CC | clevocc30: URI 拉起或广播 204 关闭 |
| 158/159 | 键盘灯开关(多型) | 同 63 |
| 166/167 | 扩展坞插拔 | OSD |
| 184/185 | GPU OC 关/开 | OSD |
| 186/187 | 亮度+蜂鸣 | 写亮度;LoopBeeper |
| 192 | 盖子动作 | 按 LIDACTION:待机/休眠/关机 |
| 199-204 | NumLk/CapsLk/ScrLk | OSD |
| 209/210 | 低电量设置/恢复 | AppSettings(303-305);SetLowBatteryLevel(25) |
| 215 | OLED 屏灭/TP hook | 屏保 / clevofnkey^1ff |
| 222/223 | 静音 | OSD |
| 234/235 | 3G | OSD |
| 240/241 | FlexiCharger 启停 | 托盘气泡 |
| 246/247/248/249 | 摄像头/摄像头+麦克 | 写 (1,6)+SetWMI(34,v);OSD |
| 252/253 | 触摸板关/开 | Win+Ctrl+F24 模拟;写 (1,10);OSD;广播 206 |
| 301/302 | FlexiAccess | OSD |
| 303/304 | GPU OC | OSD |
