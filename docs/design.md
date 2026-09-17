# 方案设计 — go-passage-of-time-mcp-server

**文档状态**：与当前代码一致（`main` 分支，2026-09-17）
**适用范围**：本文描述服务**实际如何工作**。所有结论均可在源码中核对，涉及源码处标注了 `文件:行号`。

---

## 1. 问题定义

MCP 服务器为一类特殊消费者服务：**调用方是语言模型，不是程序员**。

这带来三条约束，构成本设计的出发点：

| 约束 | 含义 | 设计后果 |
|------|------|----------|
| 模型只看得到工具描述 | 它不会读源码、不查文档、不问澄清 | 描述必须自包含；行为差异必须显式声明 |
| 模型会把结果当事实 | 它没有独立的验证手段 | 宁可报错，不可返回「看起来像结果」的错误值 |
| 模型跨会话无状态 | 上一次调用建立的心智模型不保留 | 每次调用都必须能独立解读输出 |

Python 原版 [passage-of-time-mcp](https://github.com/jlumbroso/passage-of-time-mcp) 用本地时区解析输入。Go 版**曾**照搬这一做法，结果是：同一句 `2026-04-19 14:00:00`，在服务器时区为 UTC+8 时表示一个时刻，在 UTC 时表示另一个时刻——**输入的含义取决于服务器的部署配置**。

这对人类用户是可接受的（人知道自己机器的时区）。对模型不可接受：它无法从工具描述推断出这个隐含变量，因此**无法可靠地构造输入，也无法校验输出**。

本设计要解决的核心问题由此确定：

> **让时间的含义只由输入字符串本身决定，不依赖服务器的部署环境。**

---

## 2. 核心模型：挂钟读数（wall-clock reading）

### 2.1 定义

时间值是一个**挂钟读数**——人在钟面上读到的数字，不含时区、不含 UTC 偏移。

```
"2026-04-19 14:00:00"  ⟹  4 月 19 日 14 点
```

没有第二条解释。在 UTC+8 的机器上运行、在 UTC 的机器上运行，结果相同。

### 2.2 为什么用 `time.Time` 承载

Go 的 `time.Time` 是**时间点**类型，语义上必带位置（location）。用它表示「无位置的挂钟读数」是一次有意的类型借用。理由：

- 标准库 `time` 提供了闰年、星期、月份天数、`AddDate` 等全套日历运算，自己实现只会引入新 bug
- 挂钟读数与时间点的**差值**运算语义一致（都是线性时长）
- 仅有的语义鸿沟是「相等性」与「时区偏移」，二者都被本设计主动约束（见 2.3）

这是一个**工程折中**，不是概念上的正确性主张。正确的类型应当是「无时区的日期时间」；Go 标准库没有，自行实现代价大于收益。

### 2.3 关键约束：UTC 作为纯代数框架

解析一律在 **UTC** 下进行（`tools.go:48,55,57`）：

```go
t, err := time.ParseInLocation(dateTimeFormat, opts.input, time.UTC)
```

**这不是说输入被理解为 UTC**，而是因为 **UTC 不带夏令时规则**。若用 `America/New_York` 之类的真实时区解析，`time.Time` 会引入 23/25 小时的天、不存在的本地时刻等规则，污染纯算术。

选 UTC 的效果是：得到一个**规则-free 的坐标系**，其中每一天都恰好 24 小时，加一天永远前进一天。

由此产生一条**强制性内部约定**（`tools.go:140-150`）：

```go
func wallClock(t time.Time) time.Time {
    return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

func (s *Server) now() time.Time { return wallClock(s.TimeManager.Now()) }
```

> **任何一个 `time.Time` 若要与解析结果比较或相减，必须先经 `wallClock()` 归一化。**

违反此约定的后果是真实发生过的：`LiveTimeManager.Now()` 曾返回 `time.Now().UTC()`，导致相对工具与解析结果处于不同坐标系——本地时间下「30 分钟前」被判为未来、`timeUntil` 结果偏差整整 8 小时。该缺陷已修复，并由 `TestRelativeToolsAgreeWithCurrentDateTime` 与 `TestLiveTimeManagerUsesLocalClock` 双向锁定。

### 2.4 `currentDateTime` 的取值

`CurrentDateTime` 返回**宿主本地时钟**的挂钟数字（`tools.go:142-153`）。取本地而非 UTC，是为了让该工具的输出可直接回喂给 `timeSince`/`timeUntil`——三者同框，模型无需做任何换算。

> **代价（已知）**：容器或服务器若配置为 UTC，`currentDateTime` 返回 UTC 读数。这是「无时区」设计的必然结果，非缺陷。

---

## 3. 时长模型

### 3.1 绝对时长

时长是**绝对的物理量**。`1d` 恒等于 24 小时，与日期无关（`tools.go:66,74-96`）。

因 UTC 框架不含夏令时规则，这一语义在整个日历上**无例外**——包括真实世界的夏令时切换日（2026-03-07、2026-10-31 等，均有测试锁定）。

### 3.2 `d` 单位的实现

`time.ParseDuration` 不支持 `d`。实现方式（`tools.go:101-131`）：

1. 若字符串不含 `d`/`D`，直接交给 `time.ParseDuration`（标准库为权威）
2. 否则先试标准解析；失败则把每个 `<数字>d` 段重写为等价小时数，再交给标准库
3. 其余字符原样透传，**校验与错误报告仍由标准库负责**

| 输入 | 重写为 | 结果 |
|------|--------|------|
| `1d` | `24h` | 24h |
| `1d3h` | `24h3h` | 27h |
| `1.5d` | `36h` | 36h |
| `-1d` | `-24h` | −24h |

### 3.3 符号即方向

减法不设独立工具：负数时长即减法（`server.go:110`）。工具数由 13 降至 12，模型少一次「该用哪个」的判断。

---

## 4. 接口契约

### 4.1 工具清单（12 个）

| 工具 | 参数（`*` 必填） | 幂等 | 依赖当前时间 |
|------|------------------|------|--------------|
| `currentDateTime` | — | 否 | 是 |
| `timeSince` | `dateTime*` | 否 | 是 |
| `timeUntil` | `dateTime*` | 否 | 是 |
| `timeDifference` | `firstDateTime*`, `secondDateTime*` | 是 | 否 |
| `addDuration` | `dateTime*`, `duration*` | 否 | 否 |
| `daysBetween` | `firstDate*`, `secondDate*` | 是 | 否 |
| `dayOfWeek` | `dateTime*` | 是 | 否 |
| `isWeekday` | `dateTime*` | 是 | 否 |
| `isWeekend` | `dateTime*` | 是 | 否 |
| `isLeapYear` | `year*` | 是 | 否 |
| `nextOccurrence` | `dateTime*`, `dayOfWeek*` | 是 | 否 |
| `previousOccurrence` | `dateTime*`, `dayOfWeek*` | 是 | 否 |

全部 12 个工具标注 `readOnlyHint = true`。幂等性标注区分「依赖当前时间」（否）与纯函数（是），由 `TestRegisteredToolSchemas` 断言。

**`subtractDuration` 不存在**，且测试显式断言其不被注册。

### 4.2 输入格式

| 类型 | 格式 | 说明 |
|------|------|------|
| 日期 | `YYYY-MM-DD` | 按当日 `00:00:00` 处理 |
| 日期时间 | `YYYY-MM-DD HH:MM:SS` | 秒级，无小数秒 |
| 时长 | Go duration + `d` | `30m`、`1h30m`、`2d`、`1d3h`、`-2h` |
| 星期几 | 英文名，大小写不敏感 | `Monday`、`monday` |

秒级精度是**格式约束**（`dateTimeFormat` 为 `2006-01-02 15:04:05`），不是实现缺陷。需要亚秒精度须扩展该常量。

### 4.3 输出格式

| 类别 | 形式 |
|------|------|
| 时刻 | `2026-04-19 14:30:00`（**无**偏移后缀） |
| 时长 | Go duration 字符串，如 `8h30m0s` |
| 判断 | 自然语言句，如 `2026-04-19 is a weekend.` |

**输出不含任何时区信息**，这是 4.1 契约的直接体现。

### 4.4 错误语义

工具错误通过 `CallToolResult.isError = true` 返回（MCP 规范要求），不抛协议级异常——这样模型**能看到错误并自我纠正**。

| 场景 | 消息 |
|------|------|
| 输入为空 | `input time cannot be empty` |
| 格式非法 | `failed to parse time: "..." Format must be ...` |
| 方向错误 | `The specified time is in the future` / `... in the past` |
| 有歧义 | `Both firstDateTime and secondDateTime must be provided` |

**方向错误是特性而非障碍**：`timeSince` 收到未来时刻时报错，而非返回负数——负数会被模型误读为「已过去 N」。

### 4.5 描述即契约

因模型只读描述（§1），描述承担了完整的行为声明义务。`server.go:21` 定义了共享常量：

```go
const zoneFreeNote = "Times are plain local readings with no time zone attached."
```

它被拼接到**每一个接受日期/时间的工具描述**及对应的参数描述上。`isLeapYear` 只收年份，是唯一豁免。

该不变量由 `TestEveryDateToolStatesNoTimeZone` 守护——新增工具若遗漏此声明，测试失败。

---

## 5. 架构

### 5.1 分层

```
go-potms/main.go            进程入口；默认 stdio，-port 时转 HTTP
  └── internal/handlers     路由注册（init 时挂载 /mcp）
        └── mcp/mcp.go      init 时构造 Server 并注册到 /mcp
              ├── server.go 工具注册：名称、描述、schema、注解
              ├── tools.go  工具实现 + ParseTime/ParseDuration/wallClock
              └── errors.go 领域错误类型
```

**关键点**：`mcp` 包在 `init()` 中完成注册（`mcp.go:8-13`），`main.go` 不感知具体工具。新增工具只需改动 `server.go` 与 `tools.go`。

### 5.2 时间源抽象

```go
type TimeManager interface { Now() time.Time }   // tools.go:21-23
```

仅一个方法。存在的唯一目的是**让测试可以固定「现在」**——生产用 `LiveTimeManager`，测试用返回固定值的 mock。

> **注意**：mock 使用 UTC（`tools_test.go:13-15`）。这一选择曾**掩盖** §2.3 所述的坐标系缺陷——mock 与解析结果恰好同框，错误不可见。因此相对工具的框架一致性必须用**真实时钟**测试（`TestRelativeToolsAgreeWithCurrentDateTime`、`TestRelativeToolsUseTheLocalClock`）。

### 5.3 测试金字塔

| 层 | 位置 | 规模 | 覆盖 |
|----|------|------|------|
| 单元 | `internal/handlers/mcp/*_test.go` | 26 个函数 | 解析、运算、schema 契约、错误路径 |
| 进程内协议 | 同上（`mcp_client.NewInProcessClient`） | 含于上 | 真实 MCP 握手 + `tools/list` + `tools/call` |
| 端到端 | `e2e/`（4 个文件） | 11 个函数；16 个用例 × 2 种传输 | 编译真实二进制，stdio 与 Streamable HTTP 各驱动一遍 |

端到端层**不 mock**，覆盖进程启动、握手、工具发现与参数编组——单元测试无法触达的部分。

**传输无关套件**：16 个行为用例写在 `suite_test.go` 的注册表里，由 `TestMCPOverEveryTransport` 在两种传输上各跑一遍。HTTP 此前被整体漏掉，正是因为套件只认 stdio；注册表消除了这一结构成因——**新增用例即自动覆盖两种传输**。另有 9 个传输专属测试（HTTP 的会话/状态码/SSE/并发，stdio 的流帧格式）。

HTTP 一侧驱动的是真实二进制的 `-port` 模式，因此 `main.go` 的 HTTP 分支本身也在覆盖内。

覆盖率 95.9%（`internal/handlers/mcp` 语句级）。所有回归测试均经**反向验证**：人为重新引入缺陷后确认测试失败——该手法已施于单元层、stdio 端到端层与 HTTP 端到端层三处。

---

## 6. 工程约束

### 6.1 CI

| 工作流 | 触发 | 内容 |
|--------|------|------|
| `go.yml` | `push` / `pull_request` / `workflow_dispatch` | gofmt、vet、单元、E2E、`-race`、覆盖率（Go 1.24 + stable） |
| `release.yml` | `release: published` | 交叉编译上传统，`gh release upload` |

**设计意图**：测试必须在评审时运行，而非发布时。此前 `go.yml` 仅在 `release: published` 触发，意味着所有缺陷（含「每个带时区的调用都失败」）在合入与发布全程无人拦截。

### 6.2 无外部依赖

仅 `github.com/mark3labs/mcp-go`（+ 传递依赖）与 `golang.org/x/sync`。无数据库、无网络、无文件系统状态。

`time/tzdata` **已移除**——不再加载任何 IANA 时区，二进制不需要时区数据库。

---

## 7. 权衡与已否决方案

### 7.1 已否决：时区感知（早期版本）

曾支持 `timeZone` 参数与 IANA 解析。否决理由见 §1：输入含义依赖部署环境，模型无法可靠构造与校验。**该实现在 Docker 环境下每个时区调用都失败**（运行时镜像无 tzdata），进一步印证其不可靠。

### 7.2 已否决：DST 感知的「日历天」

曾考虑 `1d` 表示「次日同一挂钟时刻」（`AddDate`）。否决理由：引入 23/25 小时特例，且因时区已知移除而无从触发。

> **代价**：服务无法表达「下个月同日」这类日历语义。`30d` 是 720 小时。若将来需要，应新增基于 `AddDate` 的工具，复用 `nextOccurrence` 已有的原语，而非重载 `addDuration`。

### 7.3 已否决：固定偏移（如一律 UTC+8）

比时区感知简单，但仍是部署配置依赖，且会静默破坏非中国时区用户的期望。优于方案 7.1，劣于当前方案。

### 7.4 保留的张力：类型借用的语义鸿沟

`time.Time` 承载挂钟读数存在概念错配（§2.2）。**不打算修复**，理由：

- 正确做法需要自定义类型并重实现全套日历运算，风险大于收益
- 错配的实际危害（意外时区运算）已由「UTC 框架 + `wallClock()` 归一化」的约定覆盖
- 该约定是**纪律性**的，非类型强制的——这是本设计最脆弱的一环，需要评审时留意

---

## 8. 已知限制

1. **`isWeekday` 不含节假日**。仅反映周一至周五，不识别法定假日与调休。
2. **`currentDateTime` 受部署时区影响**。宿主为 UTC 时返回 UTC 读数（§2.4）。
3. **无亚秒精度**。格式常量限制到秒。
4. **无日历语义运算**。「加一个月」无法表达（§7.2）。
5. **HTTP 模式的非功能面未覆盖**。`e2e/` 已覆盖 HTTP 的功能行为（握手、会话、状态码、SSE、并发会话），但**不包括**：TLS/鉴权（本服务不提供）、SSE 断线重连（`Last-Event-ID` 续传——mcp-go v0.34.0 不支持该特性，故无从测试）、多进程或反向代理下的会话粘滞。
6. **仅 linux/amd64 运行测试**。`release.yml` 构建 windows/darwin 产物但不测其行为。
7. **无并发压力测试**。`TestHTTPServesConcurrentClients` 仅验证 4 个并发会话互不串扰，不构成负载测试。

---

## 9. 相关文档

| 文档 | 内容 |
|------|------|
| [SKILL.md](../SKILL.md) | 面向调用方（含模型）的完整工具参考与实测示例 |
| [TEST_REPORT.md](../TEST_REPORT.md) | 测试报告、回归有效性验证、复现命令 |
| [README.md](../README.md) | 安装与运行 |
