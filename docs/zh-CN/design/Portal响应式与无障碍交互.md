# Portal 响应式与无障碍交互

> **翻译说明：** 本文是[英文原文](../../design/portal-responsive-and-accessible-interaction.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** Portal 与共享 GUI 贡献者 · **状态：** 已实现——五个分片(壳层与导航、
> 共享弹层、核心工作流、数据与管理、自动化回归矩阵)均已交付。验收标准中的屏
> 幕阅读器播报检查按其自身描述属于人工 QA,而非自动化分片

本文定义关键 Portal 路径在窄屏、键盘或辅助技术下保持可用的交互不变量。这是一项可
独立交付的展示改进。完整范围属于 R5；如果更早的路线图事项因缺少这项能力而无法完成
运维者路径，可以提前采用有限切片。

## 目录

- [目标结果](#目标结果)
- [证据与约束](#证据与约束)
- [支持的布局范围](#支持的布局范围)
- [交互决策](#交互决策)
- [无障碍要求](#无障碍要求)
- [实施切片](#实施切片)
- [验收标准](#验收标准)
- [被否决的替代方案](#被否决的替代方案)
- [相关记录](#相关记录)

## 目标结果

相同的关键 Portal 路径可以在桌面、紧凑和窄屏布局下完成，不出现被裁剪控件、隐藏
上下文、页面二维滚动或不可访问的 modal 与导航行为。

## 证据与约束

- 当前窄屏规则把 shell 堆叠起来，并将整个侧栏限制为很短的滚动区域。Logo、Space
  切换器、导航和账号动作因而争夺同一块垂直空间。
- Files 在内容旁使用固定宽度 tree，没有窄屏交互模型。
- 共享 modal tab 使用固定侧边栏，较长 modal 内容可能被 overflow 行为裁剪。
- 一些设置 tab 会水平滚动，但该模式并不一致，而且本身不能保证动作可见。
- Portal 浏览器测试当前未设置有代表性的窄 viewport。
- Portal 与 Desktop 有意通过 `@buildmax/gui` 共享展示。共享 primitive 必须保持与数据
  和路由无关。

## 支持的布局范围

本设计定义行为范围，而不是设备专用布局：

| 范围 | 参考宽度 | Shell 行为 |
|---|---:|---|
| 宽屏 | 1280 px 及以上 | 持久导航，适用时使用多栏内容 |
| 紧凑 | 768–1279 px | 可折叠导航，减少次要细节 |
| 窄屏 | 320–767 px | 单内容栏，导航位于 overlay drawer |

参考宽度是测试点，不是对具体设备的假设。内容必须在这些宽度之间以及浏览器缩放 200%
时继续 reflow。Feature 可以在内容确有需要时使用更早的 breakpoint；breakpoint 由布局
和组件拥有，不在每个页面重复复制。

## 交互决策

窄屏下，shell 显示包含当前 Space 与菜单控件的紧凑 header；页面自己的 H1 说明页面名称，
因此 header 不再重复（见[页面体系](portal-frontend-page-system.md#页面结构与模板)）。菜单打开无障碍
overlay drawer，内含完整的作用域导航与账号动作。选择目的地后 drawer 关闭，并以可预测
方式恢复焦点。持久桌面侧栏不会被挤进文档流。

Breadcrumb 保留当前对象和最近的有用 parent。更早祖先可以折叠进 overflow 控件，但
Space 上下文始终在 shell 可见。页面主要动作靠近标题或进入带标签的 overflow menu；
不能只因宽度而消失。

Files 在窄屏使用渐进式导航：用户看到 folder list 或选中 item 其中之一，并
有清晰 Back 动作。宽屏可以并排保留 tree 与内容。窄屏页面不要求同时水平和垂直滚动才能
选择文件。

Table 明确选择三种窄屏行为之一：reflow 为带标签的 card、保留有清晰提示的水平滚动区，
或把次要 column 隐藏到 row detail。任意裁剪不是一种行为。密集执行 trace 可在自己带
标签的区域内水平滚动，但不能让整个页面水平滚动。

Modal 在窄屏变成全高 sheet 或接近全 viewport 的 dialog。Body 滚动时 header 与主要
动作保持可达。带侧边 tab 的 dialog 改为水平 tab list 或顺序 panel。

## 无障碍要求

- Navigation drawer、menu、tab、dialog 与 disclosure 控件使用语义元素和适当 ARIA
  暴露名称、状态与关系。
- Dialog 打开时约束焦点，在安全时支持 Escape 关闭，把焦点恢复到 opener，并阻止背景
  交互。
- 所有动作都可用键盘按逻辑顺序访问，并有可见焦点指示。Reflow 后焦点顺序不得与视觉
  阅读顺序不同。
- 交互目标至少为 44 × 44 CSS pixels，除非相邻留白提供等效目标区域。
- 状态与 mutation 反馈会被播报，但不会意外移动焦点。
- 颜色不是表达状态、错误、权限或选择的唯一信号。
- 动画尊重 `prefers-reduced-motion`；在窄屏参考宽度缩放和放大文字时，不隐藏控件或
  要求页面水平滚动。

## 实施切片

1. **Shell 与导航。** 引入紧凑 header 和 drawer，再测试 Space 切换与所有主要目的地。
2. **共享 overlay。** 在 `@buildmax/gui` 修复 focus management、body 滚动、侧边 tab
   与窄屏 dialog 尺寸，不引入 Portal 策略。
3. **核心工作路径。** 在紧凑和窄屏下 reflow Chat、Issues、Issue Detail、Task Detail
   及其动作。
4. **数据与管理。** 为 Files 增加渐进式导航，并为 Artifacts、Plugins、membership、
   audit 与 administration 定义明确 table 行为。
5. **回归矩阵。** 向现有 Portal 浏览器套件添加自动 viewport 与键盘覆盖。

每个切片都可独立合并，前提是不回退已经支持的路径。组件迁移包括 focus、zoom 与窄屏
测试；单独的全局样式规则不能证明支持。

## 验收标准

- 在 390、768 和 1280 CSS pixels 下，用户可以切换 Space、启动 Chat、打开 Issue
  及其最新 run、浏览 Files 并进入 Space 设置。
- 在 390 pixels 或 200% zoom 下没有文档级水平滚动；组件局部水平区域有标签且键盘
  可操作。
- Navigation 和 modal 交互通过纯键盘测试，包括焦点进入、约束、关闭与恢复。
- 每种布局范围都能发现主要动作和当前 Space。
- 文件浏览不会在不可读的窄内容旁渲染固定侧边 tree。
- 自动浏览器覆盖运行代表性 viewport 测试，并对 shell、一个 dialog 和一个 tab 界面
  运行聚焦的 accessibility scan。
- 手动检查覆盖 route change、error、mutation completion 与 dialog title 的 screen
  reader 播报。

## 被否决的替代方案

- **缩小桌面侧栏。** 它通过让所有控件更难扫描来保留全部控件，且没有解决焦点或滚动。
- **只支持某个最小桌面宽度。** 运维工作仍会通过 tablet、分屏和缩放后的桌面发生；
  核心路径必须 reflow。
- **使用一个全局 mobile media query。** Files、table、trace 与 dialog 需要不同的交互
  决策，不能只根据宽度推导。
- **把无障碍视为后续 polish。** Drawer、modal 和 tab 架构会决定语义与焦点行为；事后
  改造会重复工作。

## 相关记录

- [Portal 导航与 Space 上下文](Portal导航与Space上下文.md)
- [Portal 状态与权限反馈](Portal状态与权限反馈.md)
- [界面定位](界面定位.md)
- [本地端到端验证](端到端测试.md)

