# GUI 样式 / 布局 / 结构优化验证记录

对应方案:`docs/GUI样式布局结构优化执行方案.md`
实施范围:`internal/gui`(源码 + 测试)与 `docs/05-UI设计规范.md`。
硬件协议、驱动、业务下发逻辑未改动。

## 1. 实施环境

| 项目 | 值 |
|---|---|
| 系统 | Windows(本机桌面会话) |
| 工具链 | `go version go1.27.3 windows/amd64` |
| 显示器 DPI | 100%(实测窗口 1400×1187 px == 1400×1187 dp) |
| 默认内容区宽度 | `defWinW 1400 - navW 200 - 2*pageSideW 40 - scrollBarW 12 = 1148 dp` |
| 最小窗口内容宽 | `minWinW 1000 → 748 dp` |
| 构建 | `go build ./...`;`go build -o Temp\bin\ccapp.exe .\cmd\ccapp` |
| 截图 | `cmd/ccshot`(`internal/win32.CaptureWindow` 抓真实窗口像素) |

## 2. 改动清单

### 2.1 P0

| 问题 | 修复 | 位置 |
|---|---|---|
| `grid2` 计算了 `gap` 却未插入间隔,两列卡片贴在一起 | 列宽按权重扣 gap 后分配,中间插入 16dp 占位;`左+16+右 == 可用宽`;任列 < 210dp 或可用宽 < 900dp 回退单列 | `internal/gui/gui.go` `grid2` |
| E 核第二列绑定 `ECores[8+i]` 却显示 1–8 | 显示编号由字段起始下标推导(`编号 = off+1`),两列参数抽成 `ecoreFieldGroups()` 供页面与测试共用 | `internal/gui/pages_hw.go` |
| 键盘图裸写 36/5 设备像素,窄卡片下键帽被压缩、右侧不齐 | 几何改由 `kbKeyH/kbKeyGap`(dp)折算;新增 `kbMinW=680dp` 最小可读宽度 + 卡片内局部横向滚动(滚轮 + 可见滚动条,命中加偏移);键宽用累计边界取整,整行正好铺满渲染宽 | `internal/gui/draw.go`、`tokens.go` |
| 键盘贴图缓存只用第一段分区色 | 缓存键纳入三段颜色 + 渲染宽 + 键高/间距 | `internal/gui/draw.go` `kbZoneKey` |
| LED 亮度滑块跟随超宽卡片拉满、色环固定 210 比例失衡 | 滑块限宽 360dp;色环 210dp 且窄卡片按可用宽收缩(下限 150dp,绘制与命中同尺寸);设置区限宽 460dp | `internal/gui/pages_mid.go`、`tokens.go` |
| 系统页三张曲线固定 186dp 并排,窄窗第三列被挤 | `curveRow` 按可用宽选 3/2/1 列,列宽锁死;曲线编辑器宽度随列宽收缩 | `internal/gui/page_system.go` |
| FlexiKey 键盘与配置档并排导致键盘被压 | `fkSplit`:右列拿不到 680dp 时改为上下排列 | `internal/gui/pages_mid.go` |

### 2.2 P1 / P2

| 项目 | 处理 |
|---|---|
| 内容最大宽度 | `scrolledPage` 限制 1440dp,超宽窗口内容左对齐(`contentMaxW`) |
| 电池数据缺失仍显示"健康" | 以 `Design > 0` 判定有无电池数据;缺失时徽章/判定显示"无电池数据",容量/循环/年限显示 `—` |
| GPU 未就绪显示 0 偏移 | `currentOffsetTxt`:未就绪且全零显示 `—` |
| CPU 频率/内存频率通道缺失显示 0 | `mhzOrDash` / `rangeOrDash` / `ratioOrDash`;利用率等真实零值仍正常显示 |
| 界面泄露实现细节 | 移除 `WMI 121/1`、`AppSettings page6/page5`、`ControlGPU.exe`、`clevofnkey^102` 等文案,改为功能性说明 |
| 节能计划页标题误写"电池" | 改为"节能计划" |
| 设计规范与实现不符 | `docs/05-UI设计规范.md` 升版 v2.2,颜色/尺寸/断点/组件状态/缺失数据规则按 `tokens.go` 与实际实现重写 |

## 3. 测试

执行命令与结果(2026-11,本机):

```text
go build ./...                     # 通过
go vet ./internal/gui/             # 通过
go test ./internal/gui/ -count=1   # 通过(含新增回归)
```

新增/更新的回归测试:

| 测试 | 守住的不变量 |
|---|---|
| `TestGrid2InsertsGapAndConservesWidth` | 多种内容宽下 `左列 + 16 + 右列 == 可用宽`,左列符合 5:4 权重 |
| `TestGrid2FallsBackToSingleColumn` | 900dp 断点以下、最小窗口内容宽、权重悬殊三种情况都必须单列;断点之上必须两列 |
| `TestCoreGroupLabelsFollowIndex` | `coreGroup{off,pre}.label(i)` 编号 = off+i+1 |
| `TestEcoreFieldGroupsCoverAllFields` | E 核两列连续覆盖 `ecoreEd[0..15]`,编号 1–16 无重复无缺口 |
| `TestKeyboardGeometryScalesWithDPI` | 键高/间距随 100/125/150/200% 缩放;宽裕时不可滚动 |
| `TestKeyboardKeepsMinWidthAndScrolls` | 窄卡片保持 680dp 渲染宽,偏移 = 680-可用宽;无限宽约束退回最小宽 |
| `TestKeyboardLabelsFitAtMinWidth` | 最小宽度下每个键帽都放得下标签(修复前此处会裁字) |
| `TestKeyboardHitMatchesGeometry` | 96 键在三种 DPI 下命中与矩形逐一对应,且每行最后一键右缘 == 渲染宽 |
| `TestPageHeightsAgainstBudget` 等既有测试 | 页面高度不失控、断点真实生效、外壳绘制正常(全部保持通过) |

已知高度变化:系统页在内容宽 1000–1100dp 区间因曲线自动降为两列而增高(页面滚动显示);
默认 1148dp 与更宽窗口下高度与改动前一致(1027dp)。

## 4. 九页截图检查(1440×1187 dp 内容宽 1148dp)

截图位于 `Temp/shots/after/`(`system/kb/fk/ch/cpu/gpu/bat/en/pref.png`),
由 `ccapp.exe -page=<页>` + `ccshot` 抓取真实窗口像素。

| 页 | 检查结论 |
|---|---|
| 系统监控 | 左右两列按 5:4 分栏且中间有 16dp 间隔;三张曲线并排完整,拖拽点与标注都在列内;页脚完整 |
| LED 设备 | 色环 210dp、亮度滑块限宽、设置区紧凑;键盘全宽显示,按键从 Esc 到小键盘完整可见 |
| FlexiKey | 配置档 / 键盘槽位两栏并排;键盘右侧无裁切,行末键缘与卡片对齐 |
| FlexiCharger | 策略与电池状态两栏,阈值滑杆宽度合理,无遮挡 |
| CPU 超频 | 实时状态/功耗电压双卡;E 核第二列显示 **E 核 9 … E 核 16**(截图 `crop-cpu-ecore.png` 已核对) |
| GPU 超频 | 资讯与偏移双卡,每行滑块带名称与当前值 |
| 电池 | 无电池数据的字段显示 `—`;健康判定不再默认"健康" |
| 节能计划 | 标题为"节能计划";监控卡与阈值/调度卡并排 |
| 偏好 | 四张设置卡组成网格,重启提示靠近 GPU 模式 |

最小窗口(1000×660 dp,内容宽 748dp)`Temp/shots/after/system-min.png`:`grid2` 正确退化为单列,
内容不被横向裁切,右缘出现垂直滚动条;FlexiKey 在 1000×760 dp(`fk-min.png`)改为上下排列,
键盘仍完整可见。

## 5. 视觉密度调整(第二轮)

第一轮只修正确性问题(间距/编号/几何),默认窗口下肉眼几乎看不出变化。第二轮针对"页面偏空、字偏小"整体上调:

| 项目 | 原值 | 现值 |
|---|---|---|
| 正文 / 次级 / 说明字号 | 12.8 / 12 / 11 sp | 14 / 13 / 12.5 sp |
| 卡片标题 / 页标题 | 13.5 / 19 sp | 15 / 22 sp |
| 卡片内边距(横/纵) | 16 / 13 dp | 20 / 18 dp |
| 卡片头高 | 46 dp | 56 dp |
| 卡片图标胶囊 | 28 dp | 34 dp |
| 双列栅格间距 | 16 dp | 20 dp(`gridGap`) |
| 控件 / 按钮高 | 34 / 32 dp | 40 / 38 dp |
| 开关 | 42×24 dp | 48×27 dp |
| 侧栏项高 / 标题栏 / 型号栏 | 38 / 46 / 26 dp | 44 / 52 / 30 dp |
| 页面内容预算(`pageBudget`) | 1057 dp | 1180 dp(默认窗口 1400×1326) |
| 键值行 | 键值同 12sp 次级色 | 键 13sp 弱色 + 值 14sp 主色加粗 |

同时新增页面级"垂直余量分布":`scrolledPage` 每帧把「视口高 − 内容自然高」写进 `pageSlack`,
页面根用 `spreadCol` 把余量均分到各段之间(单段上限 96dp),短页面不再全部挤在顶部;
余量不计入自然高度,避免逐帧振荡。

离屏核对手段:`CC_DUMP=1 go test ./internal/gui/ -run TestDumpPageScreens` 把九页渲染到
`Temp/shots/headless/*.png`(headless GPU,与真机同一 shaper/字体管线)。

### 5.1 卡片撑满窗口(垂直填充)

短页面此前只占窗口上半屏,下半屏全黑。现在:

| 机制 | 说明 |
|---|---|
| `pageSlack` | `scrolledPage` 每帧写入「视口高 − 内容自然高」 |
| `grid2Fill` | 每列一张卡的页面(电池 / GPU / 节能 / FlexiCharger / FlexiKey):两列撑到同一目标高度 |
| `fillPair` + `colFill` | 每列多张卡的页面(偏好):两列各按同一余量撑高,页面只记一次账 |
| `spreadCol` | 多段页面(系统 / LED / CPU):余量分布到各段之间(单段上限 96dp) |
| 卡片绘制 | 撑高时底色用显式高度绘制(Stack 的 Expanded 只按内容尺寸铺底),正文垂直居中 |
| 稳定性 | 余量按实际增长记账 + 1px 容差,`TestFillStability` 守住"不逐帧抖动" |

九页离屏渲染实测卡片底范围(窗口 1400×1326):电池 166→1262、GPU 166→1262、节能 166→1262、
FlexiCharger 166→1262、FlexiKey 216→1262、偏好 166→1076;系统页内容本就接近满屏,不受影响。

### 5.2 实施过程中的一次事故(已恢复)

为避免上下文继续扩散,这里如实记录:在批量替换 `gui.go` 时我误用 PowerShell 直接读写文件,
PowerShell 以 ANSI 读取 UTF-8 源码导致中文注释变成乱码,并插错了一行;随后 `git checkout -- gui.go`
又把该文件里**此前已有的未提交改动**(`txtDims` / `replayText` / `DpiScale` 等)一并回退。

恢复与验证:

1. 用 `Temp/base/internal/gui/gui.go`(本次改动前的基线快照)恢复文件主体;
2. 按当前 `tokens.go` 与页面代码重建 `windowHeightFor` / `defaultWindowSize`(改用
   `win32.PrimaryWorkAreaDp`),补回 `txtDims` / `replayText` 两个"录制一次文本、按坐标回放"的
   辅助函数(实现为 `op.Record` + `op.Offset`),并删除了与 `tokens.go` 重复的 `headerGap`;
3. `go build ./...`、`go vet ./internal/gui/`、`go test ./...` 全部通过,九页离屏渲染正常,
   表头/侧栏/型号栏的既有测试(`TestHeaderControlsNoOverlap`、`TestShellLayout` 等)均通过。

风险说明:这两个辅助函数是按调用点重建的等价实现,不是原始版本;若原实现还有额外细节
(例如对宏的复用策略),需要原作者确认。后续对 Go 源码的批量修改一律走编辑工具,不再用
PowerShell 读写。

## 6. 未验证 / 保留项

以下场景未在本机验证,属于硬件或环境依赖项,不作为"已通过"记录:

- 真实硬件写入:XTU 应用、超频偏移下发、FlexiCharger 阈值、电池校准、GPU 模式切换与重启。
- 键盘局部横向滚动只在离屏测试(几何/命中/宽度)中验证;实机滚轮交互未逐项手测
  (默认与最小窗口下 620–748dp 内容宽仍不需要滚动)。
- 200% DPI 下无实机会话(离屏几何测试已覆盖缩放一致性)。
- 深链导航(`NavNotify` 的 message-only 窗口)在本次构建中 `ccshot -pages` 找不到,截图改用
  `ccapp.exe -page=<页>` 逐个启动(`-page` 路径正常)。`internal/win32/nav.go` 本次未修改,
  该现象与既有构建差异的因果关系未确认,未纳入本次改动;`go vet ./...` 对该文件既有两处
  `unsafe.Pointer` 报 `possible misuse`(源码内已注明 WndProc 场景属预期,非本次引入)。
- `internal/gui/widgets.go` 中 `KBGrid.sz` 字段当前未使用(历史字段),未清理以免扩散改动范围。
