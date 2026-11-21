# Logo 颜色的平台边界

设置中的六种配色共用一个设备级选项；确认后更新应用内 Logo，并向系统提交对应图标，重启恢复。它不是安装包主题，不会修改已签名的可执行文件或应用包。

配色为薄荷绿、科技蓝、中国红、紫罗兰、暖橙、石墨灰。早期测试版保存的天蓝选项会迁移为中国红。

| 平台 / 位置 | 覆盖范围 | 边界 |
| --- | --- | --- |
| 所有平台：侧边栏、配色效果图 | 支持 | 同一数据目录的窗口同步；取消不应用 |
| Windows：标题栏、Alt+Tab、运行中的任务栏分组 | 支持 | 窗口图标与 Shell 图标资源同时更新；资源位于用户数据目录，退出后保留 |
| Windows：当前用户的桌面、开始菜单、启动目录、固定任务栏快捷方式 | 有条件支持 | 仅更新已存在且目标文件与当前 Motty EXE 相同的 `.lnk`；保留参数、工作目录等启动属性；按文件身份识别，不按名称识别 |
| Windows：其他快捷方式和安装图标 | 不统一覆盖 | 不扫描磁盘任意目录，不修改包装脚本、商店快捷方式、指向其他安装副本的链接、EXE 内嵌图标、安装器、卸载列表或文件关联图标；公共桌面/所有用户开始菜单等共享入口保持原样 |
| Windows：托盘 | 支持 | 使用所选颜色；系统决定是否折叠到隐藏图标区域 |
| macOS：运行中的 Dock | 支持 | 显式更新 `NSApplication.applicationIconImage`，包括从 `.app` 启动；退出后静态 Dock 启动入口可能恢复包内图标，下次启动再恢复选择 |
| macOS：菜单栏 | 保持单色 | 继续使用 template image，由系统处理深浅色和高亮，不把菜单栏染成配色 |
| macOS：Finder、Launchpad、未运行的应用/别名、DMG | 不覆盖 | 图标来自安装包/系统缓存。本功能不改 `.app` 资源或签名，不清理系统缓存 |
| Linux X11：窗口、任务切换器 | 支持提交 | 更新 `_NET_WM_ICON`；窗口管理器决定展示哪个分辨率，面板也可能优先使用 `.desktop`/主题图标 |
| Linux GTK / Wayland：任务栏、Dock、启动器 | 由桌面决定 | GTK 接收所选图标，但 Wayland 合成器通常按 app ID 查询 `.desktop`。本功能不复制或修改包管理器的启动项，不承诺覆盖启动器和固定入口 |
| Linux：托盘 | 支持提交 | 存在 StatusNotifier 宿主时更新彩色图标；没有托盘宿主的桌面不显示托盘 |

## Windows 的范围与降级

- ICO 在首次使用某个配色时从原有图片生成，包含 14 个分辨率；不同内容使用不同文件名，避免颜色变化时仍命中旧图标缓存。它们是运行时数据，不增加六套安装资源。
- 桌面和固定任务栏只检查目录本身；开始菜单检查子目录。桌面位置通过 Known Folder 查询，支持重定向桌面。不会为用户创建或固定新的快捷方式。
- 开发版、便携版、指定 `MOTTY_DATA_DIR` 的隔离配置不改正式版快捷方式；它们仍更新自己的运行窗口和任务栏。自定义启动入口的参数/环境仍由该入口负责。
- 配色是当前用户的偏好，因此不改所有用户共享的快捷方式，即使程序以管理员身份运行也一样。共享入口指向单个用户的图标文件会影响其他账户；全用户安装生成的共享入口保留安装图标，当前用户固定的任务栏入口仍在可覆盖范围。
- 不提权、不重启 Explorer、不删除系统图标缓存。资源写入失败时，窗口/托盘仍可更新，任务栏沿用系统能读取的资源；无法读取的快捷方式跳过，可识别的更新错误写入诊断日志。
- Explorer 接收文件变更通知后决定刷新时机，无法保证所有 Windows 版本与第三方任务栏立即刷新。已有固定入口若仍保留旧缓存，可取消固定并从运行中的新图标重新固定；新建的任意快捷方式需在下一次切换配色或启动时纳入检查。
- 用户删除图标数据目录后，引用它的快捷方式需要重新启动 Motty 恢复资源。卸载/清理用户数据不在此配色功能中自动改写链接。

## 验证范围

- Windows 原生测试验证真实窗口的大小 HICON、旧句柄释放，以及真实窗口 Shell 属性中的 ICO 路径和 AppUserModelID；真实 `.lnk` 测试验证目标隔离、图标位置和启动参数/工作目录保留，并执行 race 检查。
- Linux 在 Xvfb 真实 X11 服务中读取更新后的 `_NET_WM_ICON`；另外保留更新分发与 GTK 原生 pixbuf 生命周期测试。
- macOS 原生测试读取更新后的 Dock 图像像素，验证连续切换和菜单栏 template；同一测试分别从普通可执行文件与临时 `.app` 启动，交由 macOS CI 执行。
- 上述 macOS 原生测试已在 [Viva CI 的 darwin 作业](https://github.com/vivaui/viva/actions/runs/44740391800/job/136302531364) 通过，包含普通程序与临时 `.app` 两种启动方式；同一作业的 DMG 内容校验也通过。
- 原生 API/属性验证不等同于覆盖所有桌面 Shell 的视觉效果。本次修复通过原生测试与 CI 验证，未做图形界面自动化核验；没有把各版本 Windows 任务栏、Linux 所有桌面环境或 macOS Finder/Launchpad 的展示宣称为已验证。

参考：[Windows 任务栏图标属性](https://learn.microsoft.com/en-us/windows/win32/properties/props-system-appusermodel-relaunchiconresource)、[窗口 AppUserModelID 与刷新顺序](https://learn.microsoft.com/en-us/windows/win32/properties/props-system-appusermodel-id)、[Apple 运行中应用图标](https://developer.apple.com/documentation/appkit/nsapplication/applicationiconimage)、[freedesktop 启动项规范](https://specifications.freedesktop.org/desktop-entry/latest/recognized-keys.html)。
