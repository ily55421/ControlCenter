# ACPI 桥驱动协议(逆向还原)

AcpiBridge.sys / AcpiBridge1.sys 是 KMDF 内核驱动,挂接 ACPI 设备 `ACPI\CLV0001`(与 `ACPI\CLV0002`),
对外暴露设备接口类 GUID:

```
{86994C74-AD43-4812-B7E7-0C420B5C5FD7}
```

用户态(原 InsydeDCHU.dll)通过 `CM_Get_Device_Interface_ListW` 枚举该接口并 `CreateFileW`
(GENERIC_READ|GENERIC_WRITE, OPEN_EXISTING) 打开设备句柄,随后使用 4 个 IOCTL:

| IOCTL | 值 | 功能 |
|---|---|---|
| IOCTL_WMI_METHOD(读/写) | 0x322400 | ACPI WMI `_DSM` 方法求值(所有 GetWMI/SetWMI) |
| IOCTL_WAIT_EVENT | 0x322404 | 异步等待热键事件(传事件句柄) |
| IOCTL_GET_NOTIFY_VALUE | 0x322408 | 取回通知值(64 位,低 8 位 = 事件号,高位 = 数据) |
| IOCTL_APP_SETTINGS | 0x32240C | 4KB 持久设置区读写(page/offset 寻址) |

相关的 WMI GUID(嵌入在 0x322400 输入结构头部):

```
ACPIBIOS_READ  = {93F224E4-FBDC-4BBF-ADD6-DB71BDC0AFAD}
ACPIBIOS_WRITE = {0B6D7934-1EB5-453E-5825-D945072A45AA}
ACPIBIOS_EVENT = {6076EF27-160E-4686-35E5-924A50B4DEAA}
```

## IOCTL 0x322400 — WMI 方法求值

输入缓冲 0x420 字节(1056):

```
偏移   大小   内容
0x00   16     WMI GUID(93F224E4-…,原生字节序 e4 24 f2 93 dc fb bf 4b ad d6 db 71 bd c0 af ad)
0x10   4      0
0x14   4      "_DSM"
0x18   4      命令号(WMI command,如 16/70/96/103/121/122)
0x1C   2      0x0104
0x1E   4      0x01000002
0x22   0x400  载荷(Get 时清零;Set 时为调用方缓冲,一般 256 字节)
```

输出缓冲 0x40C 字节(1036):

```
偏移   大小   内容
0x00   4      ?
0x04   4      状态/返回码
0x08   4      条目数 count
0x0C   …      条目数组(WMI 数据对象):
```

每个条目:

```
+0x00  WORD type    0 = 整数(数据为 DWORD,位于 +0x04)
                  2 = 缓冲(数据为 size 字节,位于 +0x04)
+0x02  WORD size   数据长度(不含 4 字节头)
+0x04  …    data
```

遍历规则(与原生 DLL 一致):`dataLen = max(4, size)`,下一条目 = 当前 + 4 + dataLen。
`GetDCHU_Data_Integer` 取第一个 type==0 条目的 DWORD;`GetDCHU_Data_Buffer` 把 type==2 条目的数据逐字节拷入调用方缓冲(256 字节)。

> 真机佐证(COLORFUL P16 Pro IXA1,`wmitest -probe=16/12`):整数条目 `size=0x0004` 且 `+0x04` 处确实带回 4 字节值
> (WMI16 = 0x89f);缓冲条目 `size=0x0100`,正好是 256 字节 WMI 包(WMI12/17)。
> 若把 `size` 解释为"含 4 字节头",前者数据长度会变成 0(读不到值)、后者只剩 252 字节,均与实测矛盾——
> 故 `size` 为纯数据长度。本机全部只读命令 `count==1`,多条目遍历暂无硬件覆盖,但按本语义自洽。

### SetWMI 编码(Clevo 约定)

`SetWMI(command, subCommand, data)`:
```
buf[0..3] = littleEndian(uint32(data))
buf[3]    = byte(subCommand)      // 覆盖最高字节
SetDCHU_Data(command, buf, 256)
```
即:低 3 字节是数据、第 4 字节是子命令。

`SetWMIPackageEx(4, in, out)`:`in[0]` 为 WMI4 子命令号(1=EC 直访、8=BIOS 特性表、21/22=GPU 切换、24/25=FnLock、29=电源计划表、30/31=FlexiCharger 等),输入输出各 256 字节。

## IOCTL 0x322404 / 0x322408 — 热键事件

```
ACPI_HSA_WAIT_EVENT { HANDLE hsaEvent }   // 8 字节,CreateEvent(手动复位) 传入
循环:WaitForSingleObject(event, INFINITE) → ResetEvent
      → DeviceIoControl(0x322408, NULL, 0, &int64, 8) → 通知值
通知值: value & 0xFF = 事件号(Event),value >> 8 = 数据(E_Data)
```

原 DCHUService 把通知值写为事件日志 `Arg001^<value>` 广播给所有组件。

## IOCTL 0x32240C — AppSettings(4KB NV 存储)

读(输入 12 字节):
```
+0 DWORD 0
+4 DWORD 0x10000000
+8 DWORD 0
```
输出 0x1000 字节 = 全部 16 页 × 256 字节设置区;调用方再按 `page*256+offset` 切片。

写(输入 0x1008 字节):
```
+0 DWORD 1
+4 WORD  线性地址 = page*0x100 + offset
+6 WORD  长度
+8 …     数据(不足 0x1000 补齐)
```

### AppSettings 布局(核心偏移)

| 页.偏移 | 长度 | 含义 |
|---|---|---|
| 0.0 | 2 | ClevoID(字节交换后 Int16) |
| 0.4-7 | 4 | ControlCenter30 版本 |
| 0.8-11 | 4 | FnKey 版本 |
| 0.12-15 | 4 | FanSpeedSetting 版本 |
| 0.24-27 | 4 | GPUOverclocking 版本 |
| 0.28-31 | 4 | Flexikey 版本 |
| 0.36-39 | 4 | BatteryUtility 版本 |
| 1.1 | 1 | 当前电源模式 0=quiet 1=pwrsaving 2=performance 3=entertainment 4=quiet_plus |
| 1.2 | 1 | UI 语言(0 系统,1..18) |
| 1.3 | 1 | ShowOSD |
| 1.4 | 1 | 语言自动检测 |
| 1.5 | 1 | Quiet_NVPowerOff_Disable |
| 1.6 | 1 | 摄像头 CCD 状态 |
| 1.7 | 1 | CC30 运行标志 |
| 1.8 / 1.9 | 1/1 | Flexikey 键盘/鼠标过滤开关 |
| 1.10 | 1 | 触摸板状态 |
| 1.12 | 1 | Arg001 由 DCHUService 发送标志(服务运行时置 1) |
| 1.13-14 | 2 | 低电量记录(亮度/电量) |
| 1.16 | 1 | AC 模式记忆的电源模式 |
| 1.17/18(4B)/22(4B float) | | AMD 手动模式/频率/电压 |
| 1.27 | 1 | 支持低刷新率面板 |
| 1.28 | 1 | 低刷新率开启 |
| 1.30 | 1 | 上一次电源模式(AMD 切换前保存) |
| 1.32 | 1 | DTT CPU 动态开关 |
| 1.33 | 1 | PgUp/Dn 禁用(1=禁用) |
| 1.34 | 1 | 蓝光过滤强度 0..10 |
| 1.35 | 1 | 锁定电源模式 UI |
| 1.36-38 | 3 | FnLock/WinKey/FnWin(1=on,2=off) |
| 1.41 | 1 | 麦克风静音状态 |
| 1.45 | 1 | CC UI 风格 |
| 2.0-3 | 4 | 键盘灯应用版本 |
| 2.4 | 1 | KBType(0 无 1 白光 2 RGB 3 Perkey 6 RGB15 22 RGB15 自定义) |
| 2.7 | 1 | OverrideBootEffect |
| 2.8 | 1 | LedKB 应用运行标志 |
| 2.32/33/34/35 | 1/1/1/1 | KB 灯效模式/静态/速度/亮度 |
| 2.36-39 | 4 | 睡眠定时(触发 + 时分秒) |
| 2.45..107 | | Fn/Ripple/Breath/Blink/Snake/Wave/Scan/Random 色与各区(All/Left/Mid/Right/TP/Logo/LightBar)状态色 |
| 2.108-126 | 19 | X170 灯带 |
| 2.128-511 | 384 | Perkey 静态色矩阵 6 行×20 列(行距 64,列距 3+col/5) |
| 4.0-3 | 4 | 风扇应用版本 |
| 4.4/5/6/7 | 1 | InitFanMode/FanMode/FanCount/FanOffset |
| 4.8 | 1 | TurboFan 状态 |
| 4.9 | 1 | Noiseless(SlowFan) |
| 4.16-69 | 54 | 自定义 3 点风扇表(CPU/GPU1/GPU2 各 18B) |
| 4.70-77 | 8 | 各模式 FanMode/FanOffset(quiet/pwrsaving/performance/entertainment) |
| 4.80-85 | 6 | AntiDust 状态与调度 |
| 5.0-1 | 2 | NVID 标志 |
| 5.2-5 | 4 | GPU OC 默认偏移(core/mem int16×2) |
| 5.6-12 | 7 | GPU 数量/基频/就绪/驱动版本 |
| 5.16/18/20/22 | 2/2/2/2 | GPU0/GPU1 核心/显存偏移(int16 LE,负值编码见 docs/08) |
| 6.9(4B)/13(8B double)/21/22/23 | | CPU 实时频率/电压/利用率/温度/内存频率 |
| 6.31 | 1 | CPUOC 全屏 |
| 6.32 | 1 | XTU 已保存标志 |
| 6.33/37(4B)/41(4B float) | | PL1/PL2/PL时间 |
| 6.49(4B) | | CPU VR Limit |
| 6.53-60,75/76 | 1 | P 核 1-10 倍频 |
| 6.61(4B)/65(4B float)/69(4B) | | 核心电压偏移/Override/iGPU 偏移 |
| 6.73/74 | 1 | 电压模式/VR Auto |
| 6.77-92 | 16 | E 核 1-16 倍频 |
| 6.110-160 | | 默认值区(PL1/PL2/时间/VR/电压/倍频) |
| 6.224/225 | 1 | 倍频上/下限 |
| 6.226(2B)/228(2B)/230(2B)/232(2B) | | Turbo_Max/Turbo_Min/内存上限/参考时钟 |
| 6.251 | 1 | XTU Service Lock(0=服务正在执行) |
| 6.253 | 1 | XTU Info Read |
| 7.0-255 | 256 | BIOS 特性表(wmi4 sub8 镜像) |
| 8.0 | 1 | CustomerID |

## WMI 命令号一览(实测使用)

- 4:多路复用包(`in[0]`=子命令:1 EC 直访 7 AMD 手动模式 8 BIOS 特性表 9 GPU 限频表 10 PSYS 功率 11 Turbo 满载 duty 13 BatterySaver 15 ASPM 表 16 AMD PPT2.0 21/22 GPU 切换查询/设置 24/25 FnLock 键盘设置 26 PgUpDn 29 电源计划表 30/31 FlexiCharger 32 Realtek LAN 35 UI 风格 40-43 EC 风扇表 RPM 45/46 低电量 55 PD 功率 60 LightBar 163 防尘状态 222 EC 版本)
- 7:电池数据包(循环次数/容量/健康判定表达式)
- 12:风扇实时数据包(RPM/duty/温度)
- 13:平台信息包(FanCount/默认风扇表/KBType[15]/存在位[50])
- 14:自定义风扇表写入包
- 16:UWP 功能支持位图
- 17:平台信息包(CustomerID[2]/皮肤色[3..5]/GPU 色[6..8]/DC 模式[13]/3 列键盘[14]/充电阈值[208][209]/亮度表[210..220]/AMD PPT 表[208..255])
- 19:飞行模式热键
- 34:摄像头开关
- 39:白光键盘亮度
- 60:WOL 状态
- 61:白光键盘亮度回读
- 65:ADC 电流读(返回值即数据)
- 70:初始化握手
- 72:睡眠通知
- 79:EnergyStar(P-state)
- 82:BIOS 特性列表(旧)
- 84:GPU 切换状态(NVSR)
- 94:通知 EC
- 96:标志位 2(bit7 防尘 bit12 DTT bit13 AMD 手动)
- 103:RGB 键盘(0xE0000000 开关位图 / 0xF4 亮度 / 0xF0-0xF6 分区色 / 模式常量)
- 107+115/117:H2RAM 读/写
- 118:充电控制(0x01000000 时间 / 0x02000000 生效日+放电阈值 / 0x03/04 调度 / 0x05000000 使能 / 0x06000000 阈值 / sub7 Flexi)
- 121:EC 大开关(sub1 风扇模式 sub11 GPU 切换 sub14 风扇偏移 sub22 WOL sub24 睡眠定时 sub25 性能模式数据 sub26 飞行模式 sub30 Boot 灯效 sub34 恢复默认 sub39 退出 sub40/41 防尘 sub42 PROC_HOT sub44 电池实用 sub45 PgUpDn)
- 122:标志位 3(bit23 CPUOC bit15 FanLess bit18 Logo bit12 Lightbar)
