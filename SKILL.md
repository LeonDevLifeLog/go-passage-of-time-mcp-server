---
name: time-calc
description: 时间与日期计算工具集。通过 go-potms MCP 工具执行日期时间相关的计算任务，包括：日期加减时长、计算日期间隔、判断星期几/工作日/周末、判断闰年、查找特定星期几的下一个/上一个出现日期、计算时间差、计算距今过去/未来多少时间。当需要进行时间相关计算时使用。
---

# time-calc

通过 MCP 工具 `go-potms__<工具名>` 调用（go-potms 已注册为本地 MCP server，stdio 模式）。

共 12 个工具，**全部为只读计算**，不会修改任何状态。

> **本文档中的示例输出均为实测结果**，可直接作为断言使用。

## 调用说明

在会话中直接调用 MCP 工具，工具名为 `go-potms__<工具名>`（如 `go-potms__addDuration`），传入对应参数。

**注意**：没有独立的「减时长」工具。做减法时给 `addDuration` 传**负数时长**即可（如 `-2h`）。

## 输入格式规范

| 类型 | 格式 | 示例 |
|------|------|------|
| 日期 | `YYYY-MM-DD` | `2026-04-20` |
| 日期时间 | `YYYY-MM-DD HH:MM:SS` | `2026-04-20 14:30:00` |
| 时长 | Go duration + `d` | `30m`、`1h30m`、`2d`、`1d3h`、`-2h` |
| 星期几 | 英文星期名（大小写不敏感） | `Monday`、`monday` |

**只写日期时的含义**：只给 `YYYY-MM-DD`（无时间部分）时，按 **00:00:00** 处理。因此 `2026-04-19` 等价于 `2026-04-19 00:00:00`。

## 时间语义（重要）

**本服务不处理时区。** 时间就是「本地时间的字面读数」——你写 `2026-04-19 14:00:00`，就表示 4 月 19 日 14 点，工具原样读回，不带任何时区或偏移后缀。

由此推出的三条规则：

1. **不存在 `timeZone` 参数**。传了也会被忽略，不影响结果。
2. **返回值不带偏移后缀**（没有 `+0800` 这种东西），因为没有时区可标。
3. **夏令时无关**。服务不做任何夏令时调整：`1d` 永远是精确的 24 小时。跨夏令时切换日做加减，结果与普通日期完全一致。

**举例**：无论 `dateTime` 是 `2026-03-07`（美东夏令时切换前一天）还是 `2026-04-19`（普通日期），加 `1d` 的结果都是挂钟前进整一天，不会有 23 或 25 小时的特例。

**`currentDateTime` 取的是服务器宿主机的本地时钟**。因此该工具返回的读数，与你直接用 `date` 命令看到的本地时间一致——可直接用来构造 `timeSince`/`timeUntil` 的入参。

> 若你的目标系统需要时区信息（例如写入第三方待办服务），请**自行**在应用层补上。本服务只负责日期时间算术。

## 完整工具列表

| 工具 | 功能 | 参数 |
|------|------|------|
| `currentDateTime` | 获取当前时刻 | 无 |
| `timeSince` | 从过去到现在经过多久 | `dateTime` |
| `timeUntil` | 从现在到未来还有多久 | `dateTime` |
| `timeDifference` | 两个时刻相差多少 | `firstDateTime`, `secondDateTime` |
| `addDuration` | 日期时间 ± 时长（用负号表示减） | `dateTime`, `duration` |
| `daysBetween` | 两个日期相差多少天 | `firstDate`, `secondDate` |
| `dayOfWeek` | 某日期是星期几 | `dateTime` |
| `isWeekday` | 是否工作日（周一至周五） | `dateTime` |
| `isWeekend` | 是否周末 | `dateTime` |
| `isLeapYear` | 是否闰年 | `year` |
| `nextOccurrence` | 下一个星期几的日期 | `dateTime`, `dayOfWeek` |
| `previousOccurrence` | 上一个星期几的日期 | `dateTime`, `dayOfWeek` |

## 高频操作

### 日期加减时长

`go-potms__addDuration`，参数 `dateTime="2026-04-19 14:00:00"` `duration="1h30m"`
→ `New time after adding duration: 2026-04-19 15:30:00`

**减法用负号**：参数 `dateTime="2026-04-19 14:00:00"` `duration="-2h"`
→ `New time after adding duration: 2026-04-19 12:00:00`

支持的时长写法：`30m`、`1h30m`、`90s`、`2d`、`1d3h`、`1.5d`、`-2h`、`-1d`。`d` = 恰好 24 小时。

### 计算时间差

`go-potms__timeDifference`，参数 `firstDateTime="2026-04-19 09:00:00"` `secondDateTime="2026-04-19 17:30:00"`
→ `The first time is earlier than the second time by 8h30m0s`

返回三种句式：`The first time is earlier than the second time by <时长>`、`The first time is later than the second time by <时长>`、`The two times are equal.`

### 计算两个日期相差多少天

`go-potms__daysBetween`，参数 `firstDate="2026-04-01"` `secondDate="2026-04-19"`
→ `There are 18 days between 2026-04-01 and 2026-04-19.`

> 只比较**日历日期**，忽略时间部分。第二个日期较早时返回负数。

### 距某时刻过去了多久 / 还剩多久

- `go-potms__timeSince`：`dateTime="2026-04-01 10:00:00"`
- `go-potms__timeUntil`：`dateTime="2026-04-20 18:00:00"`

返回 Go duration 字符串（如 `36h15m12.3s`）。注意：

- `timeSince` 对**未来**时刻报错 `The specified time is in the future`
- `timeUntil` 对**过去**时刻报错 `The specified time is in the past`
- 这两个工具依赖服务器当前时间，**不可缓存**

### 判断星期几 / 工作日 / 周末

- `go-potms__dayOfWeek`：`dateTime="2026-04-19"`
  → `The day of the week for 2026-04-19 is Sunday.`
- `go-potms__isWeekday`：`dateTime="2026-04-20"`
  → `2026-04-20 is a weekday.`
- `go-potms__isWeekend`：`dateTime="2026-04-19"`
  → `2026-04-19 is a weekend.`

> `isWeekday` **只判断周一至周五，不识别法定节假日**。调休、春节等需自行判断。

### 查找下一个 / 上一个星期几

- `go-potms__nextOccurrence`：`dateTime="2026-04-19"` `dayOfWeek="Monday"`
  → `The next occurrence of Monday after 2026-04-19 00:00:00 is 2026-04-20 00:00:00.`
- `go-potms__previousOccurrence`：`dateTime="2026-04-19"` `dayOfWeek="Friday"`
  → `The previous occurrence of Friday before 2026-04-19 00:00:00 is 2026-04-17 00:00:00.`

> 语义为**严格**的下一个/上一个：若输入日期本身已是目标星期几，会跳到再前/后一周（例：`2026-04-19` 是周日，查下一个 Sunday 得到 `2026-04-26`）。`dayOfWeek` 大小写不敏感。

### 判断闰年

`go-potms__isLeapYear`，参数 `year=2028`
→ `2028 is a leap year.`

### 获取当前时刻

`go-potms__currentDateTime`，无参数
→ `2026-09-17 12:30:00`

> **依赖服务器当前时间，不可缓存。**

## 注意事项

1. 时间是**纯本地读数**，不含时区；需要时区请在应用层自行处理
2. **减时长用负数**（`-2h`），没有单独的减法工具
3. 不要传 `timeZone` 参数——它不存在，传了也被忽略
4. 返回值**不带** `+0800` 这类偏移后缀，这是预期行为而非 bug
5. `1d` 恒等于 24 小时，与夏令时无关
6. 只写日期时按当天 `00:00:00` 处理
7. `timeSince`/`timeUntil`/`currentDateTime` 依赖当前时间，不可缓存结果
8. `isWeekday` 不含法定节假日
