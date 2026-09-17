# 测试报告 — go-passage-of-time-mcp-server

**日期**：2026-09-17
**Go 版本**：go1.27.1 linux/amd64（`go.mod` 声明 go 1.24.4）
**测试对象**：`github.com/LeonDevLifeLog/go-passage-of-time-mcp-server`

运行方式：

```bash
go test ./...                 # 全部
go test ./internal/... -v     # 仅单元测试
go test ./e2e/ -v             # 仅端到端测试
```

---

## 0. 结论摘要

| 项目 | 结果 |
|------|------|
| 构建 `go build ./...` | 通过 |
| 静态检查 `go vet ./...` | 无告警 |
| 格式 `gofmt -l` | 无差异 |
| 单元测试 | **26 个测试函数，全部通过**，`internal/handlers/mcp` 语句覆盖率 **95.9%** |
| 端到端测试 | **11 个测试函数，全部通过**；含 16 个行为用例 × 2 种传输（stdio / HTTP）= 32 个，另加 9 个传输专属测试 |
| 竞态检测 `go test ./... -race` | 通过 |
| 回归有效性 | 已反向验证：**人为重新引入时区偏移后，stdio 与 HTTP 两侧的测试均立即失败** |

本轮变更：**将 HTTP（Streamable HTTP）传输纳入端到端套件**（此前的已知缺口），把 e2e 从「只认 stdio」重构为传输无关套件。上一轮变更：移除时区支持、合并加减时长工具（13 → 12 个工具）、新增 `d` 时长单位、修复 CI 触发条件。

---

## 1. 本轮变更

### 1.1 移除时区支持

时间改为**纯本地读数**：不含时区、不返回偏移后缀、不做任何夏令时调整。

| 移除项 | 说明 |
|--------|------|
| `timeZone` / `firstTimeZone` / `secondTimeZone` 参数 | 12 个工具的 schema 中均已无这些属性，有测试断言其不存在 |
| `TimeManager.LoadLocation` | 接口只剩 `Now()` |
| `TimeZoneLoadError` | 类型已删除 |
| `time/tzdata` 内嵌 | 不再需要，已从 `go-potms/main.go` 移除 |

`ParseTime` 现统一以 UTC 为**代数框架**解析——这只是因为 UTC 不带夏令时规则、可给出稳定无特例的算术结果，**不代表输入被理解为 UTC**。挂钟数字原样进出。

### 1.2 合并加减时长工具

`subtractDuration` 已删除，减法改用**负数时长**：

```json
{"name":"addDuration","arguments":{"dateTime":"2026-04-19 14:00:00","duration":"-2h"}}
→ "New time after adding duration: 2026-04-19 12:00:00"
```

工具总数 **13 → 12**。有测试断言 `subtractDuration` 不再被注册，且工具数恰为 12。

### 1.3 `d` 时长单位

`1d` = 恰好 24 小时（绝对时长），与 `h`/`m`/`s` 同质可相加。

| 输入 | 等价于 |
|------|--------|
| `1d` | `24h` |
| `2d` | `48h` |
| `1d3h` | `27h` |
| `1.5d` | `36h` |
| `-1d` | `-24h` |

因已无时区规则，`1d` 在所有日期上都精确等于 24 小时——包括 2026-03-07、2026-10-31、2026-03-28、2026-10-24 这些真实世界夏令时切换日（均有测试锁定）。

### 1.4 CI 触发条件

原先 `.github/workflows/go.yml` 仅在 `release: published` 时运行——**push / PR 完全不跑测试**，缺陷只会在发版或使用中暴露。

现拆分为两个工作流：

| 工作流 | 触发 | 内容 |
|--------|------|------|
| `go.yml` | `push` / `pull_request` / `workflow_dispatch` | gofmt 校验、`go vet`、单元测试、端到端测试、`-race`、覆盖率（Go 1.24 与 stable 双版本矩阵） |
| `release.yml` | `release: published` | 交叉编译各平台产物并上传到 release |

同时把已废弃的 `actions/upload-release-asset@v1`（2021 年起弃用）替换为 `gh release upload`。

---

## 2. 单元测试

**位置**：`internal/handlers/mcp/`
**规模**：26 个测试函数
**运行**：`go test ./internal/handlers/mcp/ -v`

| 测试函数 | 覆盖内容 |
|----------|----------|
| `TestParseTime` | 日期/日期时间/自定义格式解析与错误输入 |
| `TestParseTimeIsZoneFree` | **回归**：解析结果偏移恒为 0，挂钟数字原样保留 |
| `TestLocalTimeRoundTrip` | **回归**：`+0s` 往返后读数不变（原先会被服务器偏移量平移） |
| `TestLiveTimeManagerUsesLocalClock` | **回归**：`currentDateTime` 必须返回宿主本地挂钟时间；此前 `Now()` 调用 `.UTC()`，在 UTC+8 宿主上少 8 小时 |
| `TestRelativeToolsAgreeWithCurrentDateTime` | **回归**：`timeSince`/`timeUntil` 与 `currentDateTime` 必须处于同一时间框架；此前二者比较基准不同源，本地时间下会误报「未来/过去」且时长偏差整个时区 |
| `TestNoTimeZoneParameterAccepted` | **回归**：传入 `timeZone` 不改变结果 |
| `TestCurrentDateTime` | 当前时刻读数形状 |
| `TestTimeSince` / `TestTimeUntil` | 相对时长；未来/过去方向性报错；「恰好现在」 |
| `TestTimeDifference` | 三种句式（早于/晚于/相等）；仅日期与含时间混用 |
| `TestDurationSpansArePlainArithmetic` | **回归**：跨夏令时区间的差值恒为 48h（无 47/49 小时特例） |
| `TestAddDuration` | 加/减/加天数/负号/缺失参数/非法输入 |
| `TestAddDurationSubtractsWithNegative` | **合并回归**：`-2h` 等价于原减法工具 |
| `TestDurationArithmeticHasNoDSTSemantics` | **回归**：4 个真实夏令时切换日上 `1d` 均精确前进一天 |
| `TestParseDuration` | `d` 单位全量用例、负时长、非法单位 |
| `TestDaysBetween` | 日历天差值、反序为负、忽略时间部分 |
| `TestDaysBetweenHandlesDeclaredParams` | **回归**：按声明参数 `firstDate`/`secondDate` 调用成功 |
| `TestRegisteredToolSchemas` | 12 个工具的属性、必填项、只读注解；断言时区参数已不存在；断言 `subtractDuration` 已移除 |
| `TestEveryDateToolStatesNoTimeZone` | **回归**：每个接受日期的工具描述都必须声明「不含时区信息」，否则模型无法正确构造输入 |
| `TestToolCount` | 工具数恰为 12 |
| `TestDayOfWeek` / `TestIsWeekday` / `TestIsWeekend` | 星期与工作日判断 |
| `TestIsLeapYear` | 闰年规则（含 1900/2000 边界） |
| `TestNextOccurrence` / `TestPreviousOccurrence` | 上下一个指定星期几；严格语义；大小写不敏感 |

---

## 3. 端到端测试

**位置**：`e2e/`（4 个文件）
**规模**：11 个测试函数；其中主套件 16 个用例 × 2 种传输 = 32 个用例
**运行**：`go test ./e2e/ -v`

```bash
go test ./e2e/ -v -run TestMCPOverEveryTransport   # 只跑共享行为套件
go test ./e2e/ -v -run TestHTTP                    # 只跑 HTTP 传输专属
go test ./e2e/ -v -run TestMCPOverEveryTransport/http   # 只跑 HTTP 下的行为套件
```

测试会**真实编译 `go-potms` 二进制**，分别以两种方式驱动它，覆盖进程启动、握手、工具发现与参数编组——单元测试无法触达的部分。

| 文件 | 职责 |
|------|------|
| `suite_test.go` | **传输无关**的 16 个行为用例 + 跨传输一致性检查 |
| `transport_test.go` | 共享类型（`mcpTransport` 接口）与进程管理 |
| `stdio_test.go` | stdio 客户端 + stdio 专属测试 |
| `http_test.go` | Streamable HTTP 客户端 + HTTP 专属测试 |

### 3.1 传输无关套件

`TestMCPOverEveryTransport` 把 16 个用例在 `map{stdio, http}` 上各跑一遍。**新增用例写进 `transportSuite` 注册表即自动覆盖两种传输**——这是本轮的主要设计目的：此前 HTTP 正是因为「套件只认 stdio」而被整体漏掉。

| 用例 | 覆盖内容 |
|----------|----------|
| `HandshakeAndToolDiscovery` | 握手；恰好 12 个工具；每个都有描述、`object` schema、只读注解；**无任何时区参数残留** |
| `EveryToolIsCallable` | 12 个工具逐一调用，确认无「出厂即坏」 |
| `DaysBetweenRegression` | 按文档参数调用得到 18 天 |
| `WallClockIsPreserved` | **回归**：跨日边界读写不被平移（含 23:30、00:30 两个易错点） |
| `OutputsCarryNoZoneSuffix` | **回归**：输出中正则匹配不到任何 `+0800` 形式后缀 |
| `DurationDayUnit` | `1d`/`1d3h`/`2d`/`24h` 等价性；非法单位被拒 |
| `AddDurationIsTheSubtractTool` | **合并回归**：正数加、负数减、负天数减、零值为空操作 |
| `NoDaylightSavingAdjustments` | **回归**：4 个真实夏令时切换日 + 1 个普通日期，`1d` 结果完全一致 |
| `WeekdayToolsSpotChecks` | 固定日期断言与闰年边界；严格 next/previous 语义 |
| `TimeDifferencePinpoints` | 三种句式精确匹配 |
| `ErrorHandling` | 9 类非法输入返回结构化错误而非崩溃 |
| `ServerSurvivesInvalidInput` | 连续非法调用后服务仍健康 |
| `DurationOutputShape` / `CurrentDateTimeShape` | 输出格式可解析 |
| `StackedWorkflow` | 多工具串联真实场景（含加 8h 再用 -8h 回退） |
| `RelativeToolsUseTheLocalClock` | **回归**：端到端验证相对工具的框架一致性与本地时钟读数 |

`TestTransportsAdvertiseTheSameCatalogue` 断言两种传输的 `tools/list` **逐字段（含完整 JSON Schema）完全一致**——它们应是同一个服务器的两种接线方式，客户端从工具清单上不应能分辨。空清单会直接判失败，避免「两个空列表相等」的虚假通过。

### 3.2 HTTP 传输专属

驱动的是**真实二进制的 `-port` 模式**（而非进程内 handler），因此 `main.go` 的 HTTP 分支本身也在覆盖范围内。

| 测试函数 | 覆盖内容 |
|----------|----------|
| `TestHTTPInitializeMintsSession` | `initialize` 返回 `Mcp-Session-Id`（`mcp-session-<uuid>` 前缀）；两次初始化得到**不同**会话 |
| `TestHTTPRequestsWithoutASessionAreRejected` | 缺会话头 / 伪造会话 id / 只有 uuid 无前缀 → 均 **400**，且不泄露工具数据 |
| `TestHTTPRejectsMalformedRequests` | content-type 错误 / 缺失 / 非 JSON / 截断 JSON → **400**；随后同一会话仍可用 |
| `TestHTTPNotificationStream` | `GET /mcp` 升级为 `text/event-stream` 且**保持打开**（1 秒内不关闭） |
| `TestHTTPSessionTeardown` | `DELETE /mcp` → 200 |
| `TestHTTPUnsupportedMethodIsNotASilentSuccess` | `PUT /mcp` 不得返回 200/202（防止路由变更把未实现的方法伪装成成功） |
| `TestHTTPServesConcurrentClients` | 4 个并发会话互不串扰——stdio 传输做不到的事 |

响应体**两种编码都接受**（`application/json` 或 `text/event-stream`），因为规范允许服务端选择；这样断言针对的是行为而非某种巧合的编码。

### 3.3 stdio 传输专属

| 测试函数 | 覆盖内容 |
|----------|----------|
| `TestStdioWireFormat` | 逐个请求读回应：**每行恰好一个完整 JSON 对象**，无横幅、无多余输出。stdout 上一个杂散日志行就会让流失去同步，故直接对原始字节断言 |
| `TestStdioExitsWhenStdinCloses` | 客户端关闭 stdin 后进程退出，不当孤儿进程残留 |

---

## 4. 回归有效性验证（关键）

测试若非「装了也会过」，就毫无价值。因此做了**反向验证**：人为在 `ParseTime` 中重新注入一个时区偏移（模拟修复前的行为），重新运行单元测试：

```
--- FAIL: TestParseTimeIsZoneFree
    --- FAIL: TestParseTimeIsZoneFree/date_and_time
    --- FAIL: TestParseTimeIsZoneFree/late_evening_is_preserved
--- FAIL: TestParseTime
    --- FAIL: TestParseTime/Valid_date_and_time
--- FAIL: TestTimeSince
    --- FAIL: TestTimeSince/Exactly_now
--- FAIL: TestTimeUntil
    --- FAIL: TestTimeUntil/Future_date_and_time
```

测试立即捕获。验证完成后已还原代码并确认全部通过。

同一手法也验证了另一处：把 `timeSince`/`timeUntil` 的比较基准改回未归一化的宿主时钟后，

```
--- FAIL: TestRelativeToolsAgreeWithCurrentDateTime
    TimeSince("2026-09-17 12:01:47") errored: The specified time is in the future
      -- the reading "2026-09-17 12:31:47" is in the past on the local clock
      but the server considered it future, meaning the two are on different frames
```

两处均已确认可被测试捕获。

### 4.3 HTTP 传输（本轮）

新增 HTTP 覆盖同样做了反向验证，确认断言**不是摆设**。

**（1）时区偏移** — 在 `ParseTime` 中重新注入 +0800 偏移后：

```
--- FAIL: TestMCPOverEveryTransport/stdio/RelativeToolsUseTheLocalClock
--- FAIL: TestMCPOverEveryTransport/http/RelativeToolsUseTheLocalClock
```

**两种传输都捕获到**，说明 HTTP 一侧的行为断言确实在生效，而非仅握手成功就通过。

**（2）有状态会话** — 把 `NewStreamableHTTPServer` 改为 `WithStateLess(true)` 后，7 个 HTTP 专属测试全部失败（均因 `initialize` 不再返回 `Mcp-Session-Id`）。这确认了未变异时「缺会话头返回 400」的断言有意义，而非恒真。

**（3）stdio 流纯净性** — 在 `main()` 中加一行 `fmt.Println`（模拟诊断信息误写到 stdout，而非 stderr）后：

```
--- FAIL: TestStdioWireFormat
    line 1 is not a standalone JSON object: invalid character 'g' looking for beginning of value
        "go-potms starting up"
```

验证完成后三处均已还原，并确认与变异前**字节一致**（`diff` 无差异）。

### 4.4 本轮修掉的一个测试基础设施缺陷

重构过程中发现：原 `e2e_test.go` 与新增的 HTTP 启动逻辑都会对同一进程**调用两次 `cmd.Wait()`**（启动等待处的 goroutine 一次、`t.Cleanup` 一次）。`os/exec` 明确要求 `Wait` 只能调用一次，第二次会永久阻塞——表现为测试跑满超时后 panic dump 出 goroutine 栈。现所有路径统一经由 `transport_test.go` 的 `processHandle` 单点回收（代码中已注明「必须是 `cmd.Wait` 的唯一调用方」）。这也是本轮唯一一处对既有测试逻辑的实质性修正。

---

## 5. 已知限制与未覆盖项

如实列出，避免误读为「全绿即无问题」：

1. **`isWeekday` 不含节假日**。仅反映周一至周五，不识别法定假日与调休。
2. **`timeSince`/`timeUntil`/`currentDateTime` 依赖真实当前时间**，测试只能断言格式与相对性质，无法断言精确数值。
3. **`currentDateTime` 取宿主本地时钟**。`LiveTimeManager.Now()` 返回 `time.Now()`（宿主时区），因此返回值就是宿主挂钟读数。注意：容器或服务器若配置为 UTC，返回值即 UTC 读数——这是「无时区」设计的直接后果。此项曾实现为 `.UTC()`，在 UTC+8 宿主上返回少 8 小时的值，已修复并由 `TestLiveTimeManagerUsesLocalClock` 锁定。
4. **无并发压力测试**。`TestHTTPServesConcurrentClients` 仅验证 4 个并发会话互不串扰，不构成负载测试；高并发下的会话状态与资源占用未验证。
5. **HTTP（Streamable HTTP）模式已纳入端到端套件**（本轮补齐）。`e2e/` 现在同时驱动 stdio 与 HTTP 两种传输：同一套 16 个行为断言在两者上各跑一遍，另加 9 个传输专属测试。**尚未覆盖**：HTTP 的 TLS/鉴权（本服务不提供）、SSE 断线重连（`Last-Event-ID` 续传，mcp-go v0.34.0 不支持该特性）、多进程/代理场景下的会话粘滞。
6. **平台覆盖有限**。仅在 linux/amd64 上运行。`release.yml` 会构建 windows/darwin 产物，但不执行其测试。
7. **`e2e` 测试在 `go test ./...` 中一并运行**，会真实编译二进制并起真实进程；整套约 2 秒（`-race` 约 3 秒）。

---

## 6. 复现命令

```bash
go test ./... -count=1              # 全量
go test ./internal/handlers/mcp/ -v -count=1   # 单元测试明细
go test ./e2e/ -v -count=1          # 端到端（含每个工具的实测输出）
go test ./... -race -count=1        # 竞态检测
go test ./... -cover -count=1       # 覆盖率
```
