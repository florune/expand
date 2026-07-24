# expand

`expand` 是一个 Windows 优先、本地优先的开发快捷词工具。它的目标不是让你打开一个大窗口查资料，而是让已经输入的触发词立刻变成需要的命令或文本。

## 日常使用

第一次创建快捷实例：

```text
触发词  :mysql-connect-prod
名称    MySQL 生产连接
分类    mysql
模板    mysql --host={{MYSQL_HOST}} --port={{MYSQL_PORT}} --user={{MYSQL_USER}} -p {{MYSQL_DATABASE}}
变量    MYSQL_HOST=db-prod.internal
        MYSQL_PORT=3306
        MYSQL_USER=developer
        MYSQL_DATABASE=orders
关联密码 ********
```

以后在浏览器、终端或编辑器中：

1. 输入并选中 `:mysql-connect-prod`。
2. 按 `Ctrl + Alt + J`。
3. 如果快捷词存在，选中文字会直接被连接命令替换，窗口不会出现。
4. 如果快捷词不存在，expand 弹出紧凑的新建窗口，并自动填入该触发词。

文本替换使用 Windows 原生输入注入，不模拟 `Ctrl+V`，因此不依赖目标软件配置的是
`Ctrl+V`、`Ctrl+Shift+V` 还是其他粘贴快捷键。Termius、Xshell 和 Windows Terminal
会自动切换为真实键盘事件，不会再因为收到 `Ctrl+V` 而进入 quoted-insert 状态并在随后显示 `^?`。

MySQL 密码不会进入替换文本。生成的命令使用 `-p` 让客户端交互读取密码；密码可从紧凑面板限时复制。

没有选中文字时按 `Ctrl + Alt + J`，打开 520×620 的快捷搜索面板。只有点击“管理”时，窗口才展开为完整管理界面。
快捷面板中的条目单击复制展开结果，双击才会填写到最近使用的外部窗口；点击 expand 本身不会覆盖这个目标窗口。

窗口右上角 `—` 表示隐藏到后台并继续响应全局快捷键；`×` 和系统关闭按钮表示彻底退出。快速面板中按 `Esc` 也只会隐藏。

expand 只允许一个运行实例。重复启动不会创建新的后台进程，而是唤起已经运行的窗口。

窗口使用 expand 自己的无边框标题栏，不再额外显示 Windows 原生标题栏。顶部品牌和用户区域可以拖动窗口，右侧按钮区域不会触发拖动。

## 本地用户与持久化

expand 没有服务端、账号注册或网络 API。本地用户用于区分独立的加密空间：

```text
%AppData%\expand\
├── profiles.json
└── profiles\
    ├── <profile-id>\
    │   └── vault.enc
    └── <another-profile-id>\
        └── vault.enc
```

- `profiles.json` 只保存用户 ID、显示名称和时间，不包含快捷内容。
- 每个本地用户拥有独立的 Salt、数据密钥和 `vault.enc`。
- 快捷词、模板、变量值和秘密统一加密持久化。
- 不知道另一个用户的主密码就无法解密其数据。
- 锁定、切换用户或退出时清除当前解密会话。

同一个 Windows 账号下的管理员仍能删除或破坏其他用户的加密文件。需要抵御这种情况时，应使用不同的 Windows 账号。

## 已实现

- 紧凑搜索面板和可展开的管理界面。
- Windows 全局快捷键 `Ctrl + Alt + J`。
- 读取当前选中文本并识别 `:trigger`。
- 找到快捷词时直接替换，不弹窗、不附加回车。
- 找不到快捷词时自动打开预填的新建界面。
- 本地多用户创建、解锁、锁定和切换。
- 每用户独立的 Argon2id + AES-256-GCM 加密库。
- 加密持久化的普通文本、命令和带变量的连接实例。
- MySQL 密码与安全连接命令分离。
- 密码限时复制，20 秒后在剪贴板内容未变化时清理。
- 24 小时无操作自动锁定；退出程序时立即结束解锁会话。
- YAML 内置模板，可在管理界面中配置变量并保存为个人快捷词。

## 内置模板

初始版本提供 40+ 条可直接展开的模板，并按领域分组：

- MySQL：连接、进程、数据库、数据表、表空间和导出。
- Redis：连接、INFO、Cluster、SCAN 和 Key 内存。
- Kafka：消费者组、消费积压、Topic、消费、生产和 Offset 重置预览。
- Docker：容器状态、日志、Inspect、Stats、Shell 和 Compose 日志。
- Linux：SSH、端口、磁盘、内存、进程、Journal 和文件日志。
- Nginx、Git、通用日期和 Codex Debug 上下文。

内置模板不要求用户安装后立即填写配置。例如 `:mysql-connect` 可以直接展开为：

```text
mysql --host=MYSQL_HOST --port=3306 --user=MYSQL_USER -p MYSQL_DATABASE
```

`MYSQL_HOST`、`MYSQL_USER`、`KAFKA_BOOTSTRAP_SERVERS`、`CONTAINER_NAME` 等大写标记是有意保留的占位符。用户可以先直接替换文本，之后再把常用环境保存成自己的加密快捷实例。个人快捷词与内置模板同名时，个人版本优先。

个人快捷词没有额外的“类型”概念：分类只负责整理，模板决定最终内容。模板中写入
`{{MYSQL_USER}}`、`{{SSH_HOST}}` 等变量后，编辑器会自动生成对应输入项。分类既可以从已有分类中选择，也可以直接输入新名称。

## 开发与构建

要求：

- Go 1.25+
- Node.js 20+
- pnpm 10+
- Wails CLI 2.12
- Windows WebView2 Runtime

```powershell
cd frontend
pnpm install
cd ..
wails dev
```

生产构建：

```powershell
wails build -clean
```

输出文件：

```text
build/bin/expand.exe
```

测试：

```powershell
go test ./...
go vet ./...

cd frontend
pnpm run build
```

## 数据路径覆盖

- `EXPAND_LIBRARY_DIR`：模板库根目录；应用管理的内置模板会同步到其 `_builtin` 子目录。
- `EXPAND_PROFILE_ROOT`：本地用户索引和加密库根目录。

生产包已经内嵌全部模板。应用每次启动都会把当前版本同步到
`%AppData%\expand\library\_builtin`，因此复制单个 `expand.exe` 到其他目录运行时，
也不依赖仓库中的 `data` 文件夹。`_builtin` 由应用维护，不建议手动修改。

## 诊断日志

运行日志默认写入：

```text
%LOCALAPPDATA%\expand\logs\expand.log
```

日志超过 2 MiB 后轮转为 `expand.log.1`。日志记录应用启动、前端挂载、全局快捷键、用户锁定/解锁、快捷实例持久化和异常堆栈，不记录主密码、秘密值、快捷内容、选中文字或剪贴板内容。常见的密码、Token、Cookie、Authorization 和带密码 DSN 会在写入前脱敏。

如果界面无法完成挂载，应用会显示诊断提示；管理界面和错误条也可以复制日志路径。

## 安全边界

- 主密码不会保存，忘记后无法恢复。
- 不要把密码直接写进普通替换文本。
- 限时清理不能保证清除 Windows 剪贴板历史或第三方剪贴板管理器中的副本。
- 本项目未经过独立密码学审计，不用于替代成熟密码管理器保存银行、邮箱恢复码等高价值个人凭证。

详细说明：

- [架构](docs/architecture.md)
- [安全设计](docs/security.md)
- [快捷词命名](docs/naming.md)
