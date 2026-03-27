# GitChat 设计文档

## 1. 目标

GitChat 使用一个 Git repository 作为聊天日志和 Agent 实验记录的唯一事实来源。

设计目标：

- 使用 Git 的 append-only 历史保存消息、成员关系和附件。
- 用户身份通过 `main` 分支上的公钥注册。
- 每个用户独占自己的消息分支，避免多人同时写同一分支带来的冲突。
- 每个 channel 独占自己的成员分支，记录该 channel 的成员集合。
- 每条消息都显式声明它属于哪个 channel、follow 了哪些消息、reply 了哪条消息。
- 历史可离线重放，可做审计，可做 Agent 实验回放。

非目标：

- 不追求 WhatsApp 式的严格单链时间线。
- 不尝试隐藏 Git 对象、commit hash 或分支拓扑。
- 不在第一版中解决端到端加密、消息撤回、权限细粒度 ACL。

## 2. 核心对象

系统里有四类一等对象：

- 用户 `user`
- 频道 `channel`
- 实验 `experiment`
- 消息 `message`

它们分别映射为：

- 用户注册表：`main` 分支上的文件
- 用户消息流：每用户一个分支
- channel 成员流：每 channel 一个分支
- 实验流：每实验一个分支

## 3. 分支与目录模型

建议命名：

- `main`
- `users/<user_id>`
- `channels/<channel_id>`
- `experiments/<experiment_id>`

`main` 分支保存全局注册信息：

- `keys/<user_id>.pub`
- `users/<user_id>.json`
- 可选：`channels/<channel_id>.json`
- 可选：`experiments/<experiment_id>.json`

其中：

- `keys/<user_id>.pub` 保存该用户用于提交签名验证的公钥。
- `users/<user_id>.json` 保存稳定元信息，例如显示名、创建时间、公钥指纹、状态。
- `channels/<channel_id>.json` 保存 channel 的稳定元信息，例如创建者、创建时间、标题。
- `experiments/<experiment_id>.json` 保存实验的稳定元信息，例如创建者、创建时间、标题。

设计原则：

- `main` 是注册表，不承载聊天消息。
- `users/<user_id>` 只允许该用户追加自己的消息。
- `channels/<channel_id>` 只记录成员变更，不记录聊天内容。
- `experiments/<experiment_id>` 保存实验起点以及被实验聊天引用的代码快照锚点。

## 4. 身份与认证

### 4.1 用户注册

用户注册通过修改 `main` 分支完成：

1. 新增 `keys/<user_id>.pub`
2. 新增 `users/<user_id>.json`
3. 创建 `users/<user_id>` 分支

注册后的身份以 `user_id` 为稳定标识。

### 4.2 提交签名

协议层只依赖 Git commit signature 做身份认证。

规则：

- 没有有效 commit signature 的消息一律忽略。
- commit signature 必须能被 `main` 分支中登记的该用户公钥验证。
- 提交所在用户分支、签名身份和 `user_id` 必须一致。

原因：

- 你的目标是确认提交者确实持有对应私钥，避免其他人伪造 commit。
- 这个目标由 commit signature 直接满足。
- `Signed-off-by` 只是文本 trailer，不提供密码学保证，因此不作为协议条件。

如果需要，`Signed-off-by` 可以保留为可选的人类可读字段，但读取器不应依赖它做身份判定。

## 5. Channel 模型

### 5.1 channel 的定义

一个 channel 对应一个分支 `channels/<channel_id>`。

channel 分支只记录成员事件，不记录聊天消息本身。

### 5.2 channel 创建

创建 channel 时：

1. 在 `main` 分支新增 `channels/<channel_id>.json`
2. 创建 `channels/<channel_id>` 分支
3. 在 `channels/<channel_id>` 上写入第一条 commit，表示 channel 创建事件

建议第一条 commit 至少包含：

- `Channel-Id`
- `Channel-Creator`
- `Channel-Title`
- 初始成员，且只能包含创建者自己

### 5.3 channel 成员管理

这里采用协议硬约束：

- channel 创建时，初始成员只包含创建者自己。
- 只有创建 channel 的用户可以继续向 `channels/<channel_id>` 追加提交。
- 创建者后续如需添加成员，必须由其本人继续向 `channels/<channel_id>` 追加成员事件 commit。
- 这些提交只表达成员集合变化，例如 `add-member` / `remove-member`。
- 创建者不能移除自己。

这意味着 channel 分支是“成员日志”，不是“聊天日志”。

优点：

- 权限模型简单。
- channel 分支不会有多人写入冲突。

缺点：

- 创建者成为单点瓶颈。
- 创建者离线时，无法增删成员。

这是协议的固定约束，不作为可选实现。

## 6. 消息模型

### 6.1 消息承载方式

每条用户消息对应 `users/<user_id>` 分支上的一个 commit。

无附件文本消息：

- 使用 `git commit --allow-empty`

有附件消息：

- 在用户分支上写入附件文件后提交
- 附件文件由 Git LFS 管理

因此，“消息”本质上是一个带结构化 trailer 的 Git commit。

### 6.2 消息属于 channel

每条消息必须属于且只属于一个 channel。

建议在 commit trailer 中显式声明：

- `Channel: <channel_id>`

如果消息未声明 channel，或者 channel 不存在，则忽略。

### 6.3 消息可引用实验 SHA

用户发送的聊天可以引用一个实验中的某个 SHA。

建议在 commit trailer 中使用可选字段：

- `Experiment: <experiment_id>`
- `Experiment-SHA: <commit_hash>`

语义：

- `Experiment` 表示该消息关联到哪个实验
- `Experiment-SHA` 表示该消息讨论、报告或引用的是该实验中的哪个代码快照

### 6.4 消息内容

建议使用 commit message 的标题和正文承载文本内容，trailer 承载结构化元数据。

示例：

```text
讨论一下 agent replay 的索引结构

我倾向于把 channel timeline 做成物化索引，而不是直接扫所有用户分支。

Channel: research
Follows: 6f1d...,a912...
Reply-To: 52bc...
Experiment: exp_replay
Experiment-SHA: b37ac1...
```

注意：

- 消息的不可变身份直接使用 commit hash。
- `Follows` 和 `Reply-To` 都引用已有消息的 commit hash。
- `Experiment-SHA` 若存在，则引用某个实验中的 commit hash。

## 7. 实验模型

### 7.1 实验的定义

一个实验对应一个分支 `experiments/<experiment_id>`。

实验分支用于保存：

- 实验开始时的配置
- 实验开始时的代码
- 与该实验相关且需要长期保留的尝试 SHA

### 7.2 实验创建

创建实验时：

1. 在 `main` 分支新增 `experiments/<experiment_id>.json`
2. 创建 `experiments/<experiment_id>` 分支
3. 在 `experiments/<experiment_id>` 上写入第一条 commit，表示实验开始事件

该第一条 commit 是实验起点，必须包含：

- 实验配置
- 代码

### 7.3 实验中的新尝试

实验中的新尝试，是基于该实验过程中某一个已有 SHA 产生的一个新的提交。

这个新的提交：

- 不要求一开始就在 `experiments/<experiment_id>` 主线上
- 但最终必须被 merge 回 `experiments/<experiment_id>`

### 7.4 保活 merge

这个 merge 的目的不是合并代码，而是让这个新的尝试 SHA 被实验分支 ref 到，从而避免被 Git GC。

协议要求：

- merge 之后，新的尝试 SHA 必须对 `experiments/<experiment_id>` 可达
- 这个 merge 过程永远不应把任何新尝试的代码合入实验主线代码树

也就是说，这是一种“只保活对象、不集成代码”的 merge。

推荐直接使用：

```bash
git checkout experiments/<experiment_id>
git merge --no-ff -s ours <attempt_sha_or_branch>
```

这里使用 `-s ours`，而不是 `-X ours`。

原因：

- `-s ours` 会生成一个 merge commit，同时保留对方历史可达性
- merge 结果的代码树完全保留当前实验主线
- `-X ours` 只是在冲突时偏向当前分支，不能保证非冲突改动不会进入结果 tree

### 7.5 为什么需要这种 merge

因为聊天消息可以引用实验 SHA。

如果某个被引用的实验 SHA 没有被实验分支保活，它之后可能被 GC，导致：

- 聊天中的引用失效
- 实验无法回放到对应代码状态
- 后续审计无法验证聊天讨论到底对应哪一版代码

## 8. 附件模型

附件只允许出现在用户分支上，不出现在 channel 分支上。

建议路径：

- `attachments/<channel_id>/<filename>`

附件管理规则：

- 使用 Git LFS 跟踪该路径下的文件。
- 附件与消息同一次 commit 提交。
- 消息 trailer 中可选带一个附件清单摘要。
- 附件大小不在协议层限制，由 Git LFS 和部署侧策略决定。

示例：

- `Attachments: image.png,trace.json`

读取端不应把工作树状态作为事实来源，而应从对应 commit tree 解析附件列表。

## 9. 元数据与语义

你要求每个消息至少带两类 meta 信息：

- 它 follow 了 channel 中哪个或哪些消息
- 它 reply 了哪个消息

建议扩展成下面这组字段：

- `Channel`
- `Follows`
- `Reply-To`
- `Created-At`
- `Experiment`
- `Experiment-SHA`

### 9.1 `Follows`

`Follows` 表示该消息作者在发送这一刻，观察到的该 channel 的最后一个或多个消息。

它表达的是 causal 依赖，而不是全局时间顺序：

- 单个值：作者当时看到 channel 只有一个 head
- 多个值：作者当时看到 channel 存在多个并发 head

这和 Git commit 的 parent 很像，但因为消息放在用户分支而不是 channel 分支，所以必须显式写在 trailer 里。

### 9.2 `Reply-To`

`Reply-To` 表示该消息是在语义上回复哪条消息。

它只表达“回复关系”，不表达时间顺序。

因此一个消息可以：

- `Reply-To` 某条较早消息
- 同时 `Follows` 当前 channel 的最新 head

这正符合你提到的 Slack 风格：

- timeline 是一个主视图
- thread reply 是附着在任意单条消息上的子视图

### 9.3 `Experiment-SHA`

`Experiment-SHA` 表示该消息引用的实验代码快照。

它不改变聊天消息的 causal 顺序，只增加实验上下文。

### 9.4 排序

本系统不定义全局真排序，只定义 causal 排序。

具体来说：

- 每条消息只需要声明它发送时看到的 channel head 集，也就是 `Follows`
- `Follows` 诱导出一个部分有序关系
- 没有因果关系的两条消息，在协议层上就是并发消息

因此协议层保证的是：

1. 如果消息 `B` 的 `Follows` 包含消息 `A`，那么 `A` 必须排在 `B` 之前
2. 如果 `A` 与 `B` 互不可达，则它们之间没有协议定义的先后关系

展示层可以为了 UI 需要把并发消息做稳定线性化，但那只是视图排序，不是协议事实。

## 10. 有效性规则

读取器或索引器应只接纳满足以下条件的 commit：

### 10.1 用户消息有效性

- commit 位于某个 `users/<user_id>` 分支上
- commit signature 可被 `main` 分支中的 `keys/<user_id>.pub` 验证
- 指定的 `Channel` 存在
- 发送该消息时，该用户是该 channel 的成员；成员资格按历史状态判定
- `Follows` 中引用的消息都属于同一 channel
- `Reply-To` 若存在，则必须属于同一 channel
- `Experiment` 若存在，则对应实验必须存在
- `Experiment-SHA` 若存在，则必须同时存在 `Experiment`
- `Experiment-SHA` 若存在，则该 SHA 必须对对应实验分支可达

### 10.2 channel 事件有效性

- commit 位于某个 `channels/<channel_id>` 分支上
- 提交者必须是 channel creator
- commit signature 能被 creator 的公钥验证
- 事件类型合法，例如 `create` / `add-member` / `remove-member`

### 10.3 实验事件有效性

- commit 位于某个 `experiments/<experiment_id>` 分支上
- 实验第一条 commit 必须是实验开始事件
- 实验开始事件必须包含实验配置和代码
- 用于保活尝试 SHA 的 merge commit 必须让该 SHA 对实验分支可达
- 用于保活尝试 SHA 的 merge commit 不得把尝试代码树合入实验主线代码树

无效 commit 不阻止 Git 历史存在，但会被应用层忽略。

## 11. 读取模型

读取某个 channel 的 timeline 时，不扫描 `channels/<channel_id>` 以外的消息内容；channel 分支只提供成员关系。

建议读取器流程：

1. 从 `main` 读取用户公钥、channel 元信息和实验元信息
2. 从 `channels/<channel_id>` 重放成员事件，得到成员集合随时间的变化
3. 扫描相关 `experiments/*` 分支，构造每个实验分支可达的 SHA 集
4. 扫描所有 `users/*` 分支
5. 过滤出 `Channel: <channel_id>` 的有效消息
6. 验证签名、成员资格、`Follows`、`Reply-To`、`Experiment-SHA`
7. 构造 causal DAG、thread 关系以及可选的 UI 视图排序

如果仓库变大，建议维护一个派生索引：

- `refs/gitchat-index/channels/<channel_id>`
- `refs/gitchat-index/experiments/<experiment_id>`

这类索引不是事实来源，只是缓存。

## 12. 推荐的提交格式

### 12.1 用户消息 commit

```text
<subject>

<body>

Channel: <channel_id>
Follows: <commit_hash_1>,<commit_hash_2>,...
Reply-To: <commit_hash>
Experiment: <experiment_id>
Experiment-SHA: <commit_hash>
Created-At: <RFC3339 timestamp>
```

说明：

- `Follows` 可为空，但推荐至少指向作者发送时当前可见的 channel head 集合。
- `Reply-To` 可为空。
- `Experiment` 和 `Experiment-SHA` 都可为空；但如果设置了 `Experiment-SHA`，则必须同时设置 `Experiment`。
- `Created-At` 不应作为协议排序依据，只能作为 UI 辅助信息。

### 12.2 channel 事件 commit

```text
add bob to research

Channel-Id: research
Event-Type: add-member
Actor: alice
Member: bob
```

### 12.3 实验开始 commit

```text
start experiment exp_replay

Experiment-Id: exp_replay
Event-Type: create-experiment
Actor: alice
```

### 12.4 实验保活 merge commit

```text
retain attempt b37ac1 for exp_replay

Experiment-Id: exp_replay
Event-Type: retain-attempt
Retained-SHA: b37ac1...
```

## 13. 主要 Trade-off

### 13.1 为什么聊天消息不直接写到 channel 分支

当前方案选择“用户写用户分支，channel 只记成员”。

优点：

- 每个用户只写自己的分支，不会在活跃 channel 上发生多人并发 push 冲突。
- 更接近 append-only 个人日志，适合 Agent 实验与审计。

缺点：

- 读取一个 channel 时必须跨所有用户分支聚合。
- timeline 不是 Git 原生拓扑，需要应用层根据 `Follows` 重新构建。

如果改成“所有消息都写 channel 分支”，读取会简单很多，但多人并发写入冲突会明显增加。

### 13.2 为什么 channel 成员变更只允许创建者写

优点：

- 权限模型简单，容易验证。
- channel 分支天然单写者。

缺点：

- 创建者是治理中心，也是可用性瓶颈。
- 不适合民主群组或大型协作。

本设计不采用这些替代方案，因为当前目标是先保证协议简单、验证清晰、分支单写者。

### 13.3 为什么协议只要求 commit signature

优点：

- 直接验证提交者确实持有私钥
- 身份判定规则单一，不依赖额外 trailer
- 客户端实现更简单

缺点：

- 少了一层人类可读的显式声明

本设计接受这个取舍，因为你的核心需求是防伪造，不是做法律或流程意义上的 sign-off。

### 13.4 为什么协议永远不引入 `Message-Id`

本协议直接使用 commit hash 作为消息身份。

优点：

- Git 已经提供全局唯一对象标识，不需要另建一层 ID
- `Follows` 和 `Reply-To` 可以直接引用已有消息对象
- 协议更小，索引器也不需要维护额外映射

缺点：

- UI 上显示 commit hash 不够友好
- 附件路径和外部系统集成时，可读性不如独立逻辑 ID

本设计接受这个取舍，并把它作为长期协议约束。

### 13.5 顺序是 causal 顺序，不是全局真顺序

这套设计明确只承诺 causal 顺序。

原因：

- 多个用户分支可并发追加消息
- 没有中心 sequencer
- 发送者真正知道的是“自己发消息那一刻看到了哪些 channel head”

因此：

- 协议层只保证由 `Follows` 诱导出的部分顺序
- UI 层如需展示单条 timeline，可以自行做稳定线性化

这与 Slack 的 thread/timeline 模型兼容，也忠实反映了并发写入事实。

### 13.6 为什么实验分支要做“只保活、不合代码”的 merge

优点：

- 被聊天引用的尝试 SHA 可以长期保留，不会被 GC
- 实验主线不会因为一次尝试而被污染
- 多个尝试可以并行存在，而不需要彼此集成

缺点：

- 这种 merge 语义不符合常规代码协作直觉
- 读取器需要额外理解“可达但未集成”的对象

本设计接受这个取舍，因为实验系统关心的是可回放性和对象保留，而不是传统分支整洁性。

## 14. 第一版建议约束

为了尽快做出可运行实验，建议第一版采用以下限制：

- 只支持公开仓库内的公开消息，不做加密
- 只支持单一签名机制，例如 SSH commit signing
- 不支持私聊
- channel 成员变更仅限 creator
- 一个消息最多一个 `Reply-To`
- `Follows` 使用 commit hash 列表
- 消息可以可选引用一个 `Experiment-SHA`
- 实验尝试必须通过保活 merge 进入实验分支可达历史
- 删除、编辑、撤回一律不支持，只允许追加更正消息

这样协议简单、实现边界清晰。

## 15. 总结

这套方案的核心是：

- `main` 保存身份与公钥
- `users/<user_id>` 保存该用户发出的消息与附件
- `channels/<channel_id>` 保存该 channel 的成员变更
- `experiments/<experiment_id>` 保存实验起点以及被聊天引用的尝试 SHA 的保活历史
- 消息通过 `Channel`、`Follows`、`Reply-To`、`Experiment-SHA` 等元数据投影到 channel 视图中

它的优势是审计性强、并发写简单、非常适合做 Agent 行为实验记录。

它的代价是读取侧更复杂，必须同时处理 causal 聊天顺序、channel 成员历史，以及实验 SHA 的可达性和保活语义。
