# 服务器到期时间(Server Expiry)— 设计文档

- **日期**:2026-07-31
- **分支**:`custom`
- **状态**:已确认,待写实现计划
- **范围**:纯 hub + 前端,不涉及 agent / CBOR 协议

## 1. 目标

在主页 systems 表新增一列"到期",展示每台服务器的到期信息;支持手动设置开始/结束日期或标记为"长期";到期临近(7 天内)或已过期时提供一键"续期"按钮,按预设周期(月/年)把到期日往后推。

### 非目标(Out of Scope)

- 不做自动续期(无 cron、无自动续期开关)——续期由用户手动点击。
- 不在系统详情页展示(仅主页列)。
- 不改动 agent / CBOR 线协议。
- 不做告警通知(到期仅在主页列变色 + 续期按钮提醒)。

## 2. 数据模型

`systems` 集合新增 4 个可选字段(纯追加,无破坏性 schema 变更):

| 字段 | PocketBase 类型 | 取值 | 说明 |
|------|----------------|------|------|
| `expire_type` | Select | `""`(未设置)/ `permanent` / `fixed` | 三态:未配置 / 长期 / 有到期日 |
| `expire_start` | Date | `YYYY-MM-DD` | 开始日期(仅 `fixed` 有意义) |
| `expire_end` | Date | `YYYY-MM-DD` | 到期日(仅 `fixed` 有意义) |
| `renew_cycle` | Select | `month` / `year`, UI 默认 `month` (DB 无默认) | 续期周期(仅 `fixed` 有意义) |

### 为什么用 `expire_type` 而非"空 end = 长期"

若用"空 `expire_end` 表示长期",则所有未配置到期信息的旧服务器(字段全空)都会被当作"长期"显示,产生误导。`expire_type` 显式区分三态:

- 空 → 未配置(列留空)
- `permanent` → 长期(显示"长期"徽标)
- `fixed` → 有到期日(显示日期/剩余天数 + 条件续期按钮)

## 3. 数据库迁移

新增迁移文件 `internal/migrations/<时间戳>_add_expire.go`,仿 `1781654400_add_traffic_quota.go`:

- 取 `systems` 集合,`Fields.Add`:
  - `core.SelectField{Name: "expire_type", Values: []string{"permanent","fixed"}, MaxSelect: 1}`(默认空)
  - `core.DateField{Name: "expire_start"}`(可选)
  - `core.DateField{Name: "expire_end"}`(可选)
  - `core.SelectField{Name: "renew_cycle", Values: []string{"month","year"}, MaxSelect: 1}`(默认空,前端提交时填 `month`)
- `app.Save(sysCol)`
- down 函数返回 `nil`(与现有迁移一致)

> 注:若所用 PocketBase 版本的 `SelectField` 默认值设置 API 不同,改用 `TextField` + 前端约束;实现时以 `go.mod` 中的 PocketBase 版本为准。

## 4. 续期 API

### 端点

`POST /api/beszel/systems/renew?system=<id>`,绑 `excludeReadOnlyRole` 中间件(仿 `refreshSmartData`)。

在 `internal/hub/api.go` 的 `registerApiRoutes` 注册:
```go
apiAuth.POST("/systems/renew", h.renewSystem).Bind(excludeReadOnlyRole)
```

### 处理逻辑(`internal/hub/update.go` 或 `internal/hub/systems/system.go`)

1. 取 `systemID := e.Request.URL.Query().Get("system")`,空则 `BadRequestError`。
2. `sys, err := h.sm.GetSystem(systemID)`;`err != nil || !sys.HasUser(e.App, e.Auth)` → `NotFoundError`。
3. 校验 `expire_type == "fixed"` 且 `expire_end` 非空,否则 `BadRequestError("System has no expiry date")`。
4. 读 `renew_cycle`,默认 `month`。
5. 计算新到期日:
   - `today := time.Now().Truncate(24h)`(或取当天 00:00)
   - `base := expireEnd`;若 `expireEnd.Before(today)` 则 `base = today`
   - `month` → `base.AddDate(0, 1, 0)`;`year` → `base.AddDate(1, 0, 0)`
6. 写回记录 `expire_end = newEnd`,`app.Save(rec)`。
7. 返回 `e.JSON(200, map[string]any{"expire_end": newEnd.Format("2006-01-02")})`。

### 续期日期计算规则

- **未过期(7 天内提前续)**:`expire_end` 仍在未来 → 从原到期日 +1 周期。
- **已过期**:`expire_end` 在过去 → 从今天 +1 周期(避免加完仍在过去,需多次点击)。
- **月底边界**:Go `AddDate` 会溢出(如 1/31 + 1 月 = 3/3)。对个人监控场景可接受;实现时记录此行为,必要时再 clamp 到月末。

## 5. 主页"到期"列(前端)

新增列定义于 `internal/site/src/components/systems-table/systems-table-columns.tsx`,仿 `monthlyTraffic` 列。

### 关键区别

`monthlyTraffic` 列需从单独的 nanostore(`$trafficSummaries`,30s 轮询 `/traffic/all`)取数据;**到期列直接读 `row.original`(SystemRecord)上的字段,无需额外 store / 轮询**。

### 列定义

- `id: "expiry"`,`name: () => t`到期``
- `Icon: CalendarClock`(lucide-react)
- `size: 50`,`hideSort: true`(或按 `expire_end` 排序,undefined last)
- `cell(info)`:
  - `const sys = info.row.original`
  - `expire_type` 空 → `return null`(列留空)
  - `expire_type === "permanent"` → 灰色徽标 `长期`
  - `expire_type === "fixed"`:
    - 算 `days = ceil((expireEnd - today) / 24h)`(可为负)
    - `days > 30` → 默认色:`{date}` + 小字 `({days}天)`
    - `7 < days <= 30` → 琥珀色(`text-yellow-500`)
    - `days <= 7`(含负)→ 红色(`text-red-500`)+ 续期按钮
    - `days < 0` → 红色 `已过期 ({-days}天前)` + 续期按钮

### 续期按钮

- 小按钮(`<Button variant="outline" size="sm">`),仅当 `days <= 7` 时渲染。
- 点击:调 `pb.send("/api/beszel/systems/renew", { method: "POST", params: { system: sys.id } })`,成功后用返回的新 `expire_end` 更新本地记录(或触发 systems 列表刷新)。
- loading / 错误状态:按钮 disabled + 文案变化;失败 toast。
- `renewWindowDays = 7` 为模块常量。

## 6. 编辑入口(SystemDialog)

`internal/site/src/components/add-system.tsx` 的 `SystemDialog`(新增/编辑共用),在 traffic 字段同区(`traffic_reset_day` 之后)新增:

- **到期类型** 单选(三选一,对应 `expire_type`):
  - 无(空)→ 不存到期字段
  - 长期(`permanent`)→ 隐藏日期/周期字段
  - 到期日(`fixed`)→ 显示下方字段
- 当选"到期日"时显示:
  - 开始日期 `<Input type="date" name="expire_start">`
  - 结束日期 `<Input type="date" name="expire_end">`(必填)
  - 续期周期 `<select>` 月 / 年,默认月(`name="renew_cycle"`)
- `handleSubmit` 中按所选类型设置/清空字段:
  - "无":`data.expire_type = ""`,`expire_start/end/renew_cycle = ""`
  - "长期":`data.expire_type = "permanent"`,清空日期
  - "到期日":`data.expire_type = "fixed"`,`renew_cycle` 默认 `month`
- 编辑现有系统时,按记录的 `expire_type` 初始化单选状态。

## 7. 类型声明

`internal/site/src/types.d.ts` 的 `SystemRecord` 增加:

```ts
expire_type?: "" | "permanent" | "fixed"
expire_start?: string   // YYYY-MM-DD
expire_end?: string     // YYYY-MM-DD
renew_cycle?: "month" | "year"
```

> 这些是 `systems` 集合的顶层字段(同 `traffic_quota`),不在 `SystemInfo`(实时统计)内。

## 8. i18n

新增可翻译字符串(lingui `extract`,30 个语言目录):

- 列标题:`到期`(Expiry)
- `长期`(Permanent / Long-term)
- `续期`(Renew)
- `还剩 {n} 天`({n} days left)
- `已过期 {n} 天前`({n} days ago)
- 到期类型:无 / 长期 / 到期日(None / Permanent / Fixed expiry)
- 续期周期:月 / 年(Month / Year)
- 开始日期 / 结束日期(Start date / End date)

主翻译(en)填英文,其余语言由 Crowdin 处理(与现有流程一致)。

## 9. 向后兼容性

- **纯 hub + 前端改动**,无 agent / CBOR / SSH 协议变更,不涉及 `MinVersion*` 常量。
- 旧系统记录无到期字段 → `expire_type` 空 → 列留空,不受影响。
- 旧 hub + 新前端 / 新 hub + 旧前端:字段为可选,前端 `?.` 读取,无不兼容。

## 10. 测试

### 服务端单测(`internal/hub/`)

续期日期计算逻辑(提取为纯函数便于测试,如 `computeRenewal(end, cycle, now)`):

- 月周期:`fixed` 未过期 → `end + 1 月`
- 年周期:`fixed` 未过期 → `end + 1 年`
- 已过期 → 从 `today + 周期`
- 月底边界(1/31 + 1 月)
- `expire_type != fixed` → 拒绝(BadRequest)

> 注:`internal/hub` 包测试本机(Windows)因 `agent_connect_test.go` 导入 agent 的 LHM 嵌入需 dotnet,可能无法直接跑;按既有惯例用 `GOOS=linux go build ./...` + `go vet` 验证编译,逻辑测试在 Linux 主机最终确认。

### 前端

无前端测试运行器(项目约定),靠 Biome `check` + `vite build` 验证。

## 11. 文件清单

| 文件 | 改动 |
|------|------|
| `internal/migrations/<ts>_add_expire.go` | 新增:systems 加 4 字段 |
| `internal/hub/api.go` | 注册 `/systems/renew` 路由 |
| `internal/hub/update.go` 或 `internal/hub/systems/system.go` | `renewSystem` handler + `computeRenewal` |
| `internal/hub/update_test.go`(或新建) | 续期计算单测 |
| `internal/site/src/components/systems-table/systems-table-columns.tsx` | 新增 expiry 列 + 续期按钮 |
| `internal/site/src/components/add-system.tsx` | SystemDialog 到期字段 |
| `internal/site/src/types.d.ts` | SystemRecord 加字段 |
| `internal/site/src/locales/*/` *.po | i18n 新字符串 |

## 12. 已知取舍 / 注意事项

- **续期按钮仅 `days <= 7` 出现**:满足"长周期服务器不常年挂按钮"的需求;提前 >7 天想续期需编辑记录改日期。
- **无自动续期 / 无 cron**:符合用户简化诉求;到期仅靠主页列变色 + 按钮提醒。
- **月底溢出**:Go `AddDate` 行为,暂接受。
- **`expire_type` 三态**:显式区分未配置 / 长期 / 有日期,避免空字段歧义。
- **个人身份硬编码**不涉及此功能;`renewWindowDays=7` 为代码常量,可按需调整。
