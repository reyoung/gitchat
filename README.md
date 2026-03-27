# gitchat

`gitchat` 是 GitChat 的本地核心实现，当前包含：

- 一个可复用的 Go 核心库
- 一个 CLI
- 一个基于 Wails 的桌面 GUI
- 一个基于 SQLite 的本地缓存层

当前假设：

- Git 历史不会被篡改
- 本地 SQLite 只是缓存，不是事实来源
- 事实来源始终是 Git repository

## 当前能力

- 初始化本地缓存数据库
- 扫描 Git refs 并增量索引到 SQLite
- 创建用户
- 创建 channel
- 向 channel 添加成员
- 创建 experiment
- 保活 experiment attempt SHA
- 发送消息，可选引用 experiment SHA
- 列出已索引的 channels
- 列出已索引的 experiments
- 按 channel 列出消息
- 启动类 Slack 的桌面 GUI

## 目录

- `cmd/gitchat`: CLI 入口
- `app`: 应用层逻辑，例如索引器
- `gitrepo`: Git 读取封装
- `store`: SQLite 缓存层
- `model`: 领域模型
- `docs/DESIGN.md`: 协议与系统设计

## 用法

```bash
go run ./cmd/gitchat index
go run ./cmd/gitchat users create
go run ./cmd/gitchat channels create --channel research --title Research
go run ./cmd/gitchat channels add-member --channel research --member bob
go run ./cmd/gitchat experiments create --experiment exp1 --title "Experiment One"
go run ./cmd/gitchat experiments retain --experiment exp1 --ref <sha>
go run ./cmd/gitchat messages send --channel research --subject hello --body world
go run ./cmd/gitchat channels list
go run ./cmd/gitchat experiments list
go run ./cmd/gitchat messages list --channel research
go run ./cmd/gitchat gui
```

默认缓存文件位置：

```text
.git/gitchat/cache.db
```

也可以手动指定：

```bash
go run ./cmd/gitchat index --repo /path/to/repo --db /tmp/gitchat.db
```

也可以在当前目录或其父目录放一个最近优先的 `.gitchat` YAML 配置文件：

```yaml
repo: /path/to/repo
db: ~/tmp/gitchat.db
user:
  name: josephyu
  key: ~/.ssh/id_ed25519.pub
```

程序会从当前目录开始向上搜索最近的 `.gitchat` 文件。命令行参数优先级高于配置文件。

如果没有显式配置用户：

- 默认用户名取当前 Linux/macOS 用户名
- 默认 key 会优先尝试 `~/.ssh/id_ed25519.pub`，再尝试 `~/.ssh/id_rsa.pub` 等常见公钥文件

因此常见情况下可以直接运行 `users create`、`channels create`、`messages send`，不用每次传 `--user`、`--creator`、`--actor`、`--key`。

## 后续方向

- 增加更严格的协议有效性校验，而不只是结构化索引
- 增加更完整的 GUI 交互体验和更多校验反馈
- 提供可复用给 GUI 的稳定应用层 API
