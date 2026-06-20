# Custom 分支修改说明

本文档汇总 `custom` 分支相对于 `main`(上游 `henrygd/beszel`)所做的全部修改,供个人维护与后续合并上游时参考。

- **分支策略**:`main` 保持干净、仅用于跟踪上游(fast-forward);所有自定义修改都在 `custom` 分支。
- **定位**:个人使用,覆盖约 10 台服务器。
- **放弃的功能**:最初的 "uptime-kuma-like" 监控未做。
- **本文档不推送至 GitHub**,仅本地参考。

修改按"功能/修复批次"组织,每个条目注明:作用、关键文件、是否含测试、向后兼容性。

---

## 一、三大新增功能

### 1. Top Processes(占用 CPU 最高的进程表)

- **作用**:系统详情页实时展示 CPU 占用最高的进程(PID、用户、命令、CPU%、内存%),仅在实时通道(1s)刷新,**不落库、无历史**。
- **关键文件**:
  - `agent/processes.go` — `processSampler`,从 gopsutil 读取每进程 CPU 时间做差分,取 top 10。
  - `internal/entities/system/process.go`、`system.go` — `Process` 结构体(CBOR 字段 0-4)、`CombinedData.TopProcesses`(CBOR 字段 5,追加)。
  - `internal/hub/systems/system_realtime.go` — 实时广播携带 top 数据。
  - `internal/site/src/components/processes-table/` — 前端表格。
- **依赖**:`pid: host`(Docker agent 需宿主 PID 命名空间才能看到全部进程),已写入 compose 与 add-system 安装片段。
- **测试**:无专门测试(采样器依赖 /proc)。
- **兼容性**:CBOR 仅追加字段,旧 hub/agent 不受影响。

### 2. Monthly Traffic Quota(月度流量配额)

- **作用**:按系统配置流量配额(GiB)与重置日(1-28),累加上下行流量,周期重置,超额/接近超额时发告警。
- **关键文件**:
  - `internal/hub/systems/traffic.go` — 周期计算、逐网卡差分累加、80% 预警、超额告警、`TrafficSummary`、`TrafficSummariesForUser`(批量)。
  - `internal/hub/api.go` — `GET /api/beszel/traffic`(单系统)、`GET /api/beszel/traffic/all`(批量,首页列用)。
  - `internal/migrations/1781654400_add_traffic_quota.go` — `systems` 加 `traffic_quota`/`traffic_reset_day` 字段;新建 `traffic_monthly` 集合。
  - `internal/site/src/components/traffic-card/` — 系统详情页流量卡片(30s 轮询累计 + 实时速率)。
  - `internal/site/src/components/systems-table/systems-table-columns.tsx` — **首页 Traffic 列**(迷你进度条,已用/配额,样式与 CPU/内存/磁盘列统一,80%/100% 变色)。
  - `internal/site/src/lib/traffic-summaries.ts` — 首页批量流量摘要 nanostore(30s 轮询 `/traffic/all`)。
- **数据源**:agent 的 `Stats.NetworkInterfaces`(每网卡累计字节)。
- **测试**:`traffic_test.go`(`TestComputeNetDelta`、`TestShouldWarnQuota`)、`traffic_all_test.go`(`TestTrafficAllEndpoint`)。
- **兼容性**:旧 agent 不上报 `NetworkInterfaces` 时不累加(卡片隐藏);字段均为追加。

### 3. Ping / Latency Monitors(延迟监测)

- **作用**:全局配置 ping 目标(host + port),hub 推送到每个 agent,agent 用 **TCP 拨号**(非 ICMP)测延迟/丢包,结果实时展示 + 落库做历史折线图。
- **关键文件**:
  - `agent/ping.go` — `pingManager`,10s 探测周期,2s 超时,并发探测,保留 12 样本环。
  - `internal/hub/systems/monitors.go` — `loadMonitorConfig`、`PushConfigToAll`、`pushConfigToSystem`。
  - `internal/hub/api.go`、`system_manager.go` — monitors CRUD 事件钩子 → 推送配置;`/api/beszel/traffic` 等路由。
  - `internal/migrations/1781654401~1781654404` — monitors 集合、访问规则、autodate 字段、唯一 host:port 索引。
  - `internal/entities/system/ping.go`、`system.go` — `PingResult`(CBOR 0-3)、`Stats.Pings`(CBOR 36,持久化)、`CombinedData.PingResults`(CBOR 6,实时)。
  - `internal/site/src/components/routes/settings/monitors.tsx` — ping 目标设置页。
  - `internal/site/src/components/routes/system/charts/latency-chart.tsx` — 每系统延迟折线图。
- **测试**:`monitors_test.go`(排序回归、唯一 host:port)、`records_averaging_test.go`(pings 滚动平均)。
- **兼容性**:旧 agent 无 `SetConfig` 处理器,hub 在 Debug 级别吞掉 `unknown action`;CBOR 仅追加。

---

## 二、Fork 安装 / 自更新 / Docker 基础设施

- **二进制安装与自更新**:`supplemental/scripts/install-agent.{sh,ps1}` 支持 `--repo owner/repo` 从 fork 安装;`internal/hub/update.go`、`agent/update.go`、`internal/ghupdate/` 的自更新读 `AGENT_REPO` 从 fork releases 拉取。
- **环境变量**:
  - `AGENT_REPO`(`owner/repo`)— 指定 fork 仓库(影响安装片段、自更新源、hub 更新角标)。
  - `AGENT_IMAGE`— add-system 安装片段用的 agent 镜像(默认 `henrygd/beszel-agent`)。
  - `AGENT_MIRROR`— 仅作用于首次二进制安装的前缀代理(如 `ghfast.top`)。
- **安装片段**:`internal/site/src/components/install-dropdowns.tsx` 按 `AGENT_REPO`/`AGENT_MIRROR`/`AGENT_IMAGE` 生成 fork 专属安装命令(含 `pid: host`)。
- **Docker**:
  - `docker-compose.yml`(源码构建 hub + 本地 agent)、`docker-compose.images.yml`(预构建镜像)。
  - `internal/dockerfile_hub_source` — 三阶段构建(bun 构建 UI → go 构建 hub → scratch)。
  - agent 镜像用 `network_mode: host` + `pid: host`。
- **CI**:`.github/workflows/docker-images-fork.yml` — push 到 `custom` 即构建 `linux/amd64 + linux/arm64` 多架构镜像到 GHCR(`:custom`、`:custom-<sha>`)。
- **goreleaser**:`.goreleaser.yml` 改 `draft: false`(fork 需要非草稿 release 才能让 `releases/latest` 指向 fork);fork 不发布 scoop/brew/winget。
- **关键文件**:`internal/hub/server.go`(env 注入)、`internal/site/src/lib/utils.ts`(前端读取)、`supplemental/scripts/`、`.github/workflows/`、`.goreleaser.yml`。

---

## 三、修复批次(按优先级)

### P0(核心缺陷)

1. **Ping 配置推送到 SSH-only agent**(`0166e4fb`)
   - `monitors.go` 的 `PushConfigToAll` 原先跳过无 WebSocket 连接的系统,导致只走 SSH 的 agent 永远收不到 ping 配置。去掉守卫,`sys.request` 自动 WS→SSH 回退。新增 `configPusher` 接口缝便于测试。
2. **Hub 更新角标 honoring fork**(`719dd300`)
   - `getUpdate` 原先永远查上游 `henrygd/beszel`,导致 fork hub 误报"有新版本"并链接到上游。改为按 `AGENT_REPO` 构建 API URL;未设时行为不变。
3. **Fork 自更新 `--china-mirrors` 脚枪**(`c47cc407`)
   - `gh.beszel.dev` 镜像只代理 henrygd 仓库,fork 用会 403。新增 `shouldUseMirror`:非上游仓库时忽略 mirror flag,fork 直连 GitHub(需代理时走标准 `HTTPS_PROXY`,零代码)。

### P1(准确性)

4. **流量多网卡虚高**(`70f373a4`)
   - 原先把所有网卡累计求和再差分,单网卡重置/增删网卡会大幅高估。改为逐网卡差分(`computeNetDelta`,内存基线 `System.lastNetIfaces`),处理重置/新增(种子)/移除。
5. **流量速率归零冻结**(`d654ae7c`)
   - `Bandwidth` 字段 `omitzero`,速率 `[0,0]` 时 JSON 不输出 `b`,卡片卡在最后非零值。前端改为 `data.stats?.b ?? [0,0]`。
6. **删 ping target 残留历史线**(`cdd54225`)
   - `latency-chart.tsx` 原先 union 全历史 target id,删掉的 target 仍画线。改为按当前 `monitors` 列表过滤。
7. **注释订正**(`5c9c843a`)
   - `PingResult` 已持久化(非 realtime-only);`ping.go` 当前延迟语义注释(最后探测失败则 0)。

### P2(体验)

8. **Top Processes 命令行宽度**(`0f6b1aa5`)— `cmdDisplayLimit` 60→120,tooltip 能看到更完整的命令。
9. **Ping TCP 语义说明**(`df1bedd8`)— 设置页加一行说明(非 ICMP,端口关闭/被墙显示 100% loss)。
10. **80% 流量预警**(`32b188a1`)— `shouldWarnQuota` + 内存内每周期一次性标记 + `notifyQuotaWarning`,沿用既有超额通道。

### P3(安全/健壮性)

11. **tar 解压路径穿越防护**(`f1fdec5f`)— `extractTarGz` 原先无 Tar-Slip 检查(zip 路径有)。新增 `sanitizeExtractPath`。
12. **自更新 SHA256 校验**(`c643c538`)— 下载 `beszel_<ver>_checksums.txt`,校验 asset 哈希后才解压/替换;失败保留旧二进制(与安装脚本一致,信任模型为 GitHub+TLS)。
13. **替换前冒烟测试**(`7bd8e831`)— 对解压出的二进制跑 `--version`,失败则不替换,避免坏二进制顶掉好服务。
14. **AGENT_REPO 畸形警告**(`a522fcb5`)— `AGENT_REPO` 无斜杠时 hub/agent 自更新 stderr 警告(原静默回退上游)。
15. **唯一 host:port**(`4763c590`)— 新 migration 去重 + 唯一索引(原始 DDL drop/recreate,因 PocketBase `AddIndex` 不重建已存在的 SQL 索引);UI 防重。

---

## 四、其他基础设施改动

- **CLAUDE.md / AGENTS.md**:`a03f3aeb` 加入项目指南与 fork 工作流说明。
- **Docker 源码 compose**:`32b2a8ee`、`1df31893` 源码构建 hub + 本地 agent 的 compose。
- **CI 多架构**:`53c51d81`、`05706bde` fork 镜像构建到 GHCR。
- **项目权限白名单 / 构建卫生**:`111586c1`、`3381a2ca`。
- **i18n**:`58481d4b`、`0dedaff5`、`62f321c5` 提取 Top Processes / 流量配额 / ping 目标的可翻译字符串(30 个语言目录)。

---

## 五、数据库迁移清单

均为追加,无破坏性 schema 变更:

| 迁移文件 | 作用 |
|---|---|
| `1781654400_add_traffic_quota.go` | `systems` 加流量字段;建 `traffic_monthly` 集合 |
| `1781654401_add_monitors.go` | 建 `monitors` 集合(非唯一 host:port 索引) |
| `1781654402_fix_monitors_rules.go` | 修正 monitors 访问规则(已部署 DB) |
| `1781654403_add_monitors_autodate.go` | 补 created/updated autodate(已部署 DB,解决 sort=created 400) |
| `1781654404_monitors_unique_host_port.go` | 去重 + host:port 唯一索引 |

---

## 六、协议 / 实体字段编号(CBOR)

仅追加,未改既有编号:

- `CombinedData.TopProcesses` = CBOR **5**
- `CombinedData.PingResults`(实时) = CBOR **6**
- `Stats.Pings`(持久化) = CBOR **36**
- `Process`(0-4)、`PingResult`(0-3) 均为新结构体。

未提升 `MinVersion*` 常量(均为 additive + omitempty)。

---

## 七、已知遗留 / 注意事项

- **`internal/hub` 包测试本机无法执行**:该包测试二进制经 `agent_connect_test.go` 导入 `agent`,而 `agent` 的 Windows 专用 LHM 嵌入(`agent/lhm/bin/Release/net48`)需 `dotnet` 构建(本机未装)。相关测试(`TestUpdateApiURL`、`TestAgentRepoMalformed`)已通过 `GOOS=linux` 编译 + 逻辑审查验证;建议在 Linux 主机跑一次完整 `make test` 做最终确认。
- **`internal/ghupdate` 偏离上游**:P0/P3 多处改动(shoudUseMirror、checksum 校验、tar-slip、冒烟测试),合并上游 `ghupdate` 时需重新应用这些加固。
- **自更新信任模型**:校验 release checksums 文件(信任 GitHub+TLS),无签名;tar-slip 已防;坏二进制不会替换好服务。完整信任链(签名)未做。
- **逐网卡流量基线在内存**(`System.lastNetIfaces`):hub 重启首周期重新种子(跳过 ~60s),`bytes_up/down` 累计真值不受影响,无重复计数。
- **80% 预警在内存**(`System.trafficWarned`):hub 重启若已 ≥80% 会重发一次;超额 `notified` 持久化,不重发。
- **个人身份硬编码**:`glh08/beszel`、`ghfast.top`、`FORK_BRANCH="custom"` 散落在 compose/install 片段;换 owner/代理需全局搜改。

---

## 八、提交总览

custom 分支相对 main 共 **56 个提交**(含设计文档与实现计划)。按批次:

- 基础设施 + 三大功能:30 个(commit `a03f3aeb` ~ `fb84f2dc`)
- P0 修复:3 个 + 文档(`851e4b2d` ~ `0166e4fb`)
- P1 修复:4 个 + 文档(`be0ce42f` ~ `5c9c843a`)
- P2 修复:3 个 + 文档(`d01475dc` ~ `df1bedd8`)
- P3 修复:5 个 + 文档(`81baf2ca` ~ `4763c590`)
- 首页 Monthly Traffic 列:2 个(`6376e26d` 批量接口 + `071b1b97` 前端列)

设计文档与实现计划存放于 `docs/superpowers/specs/` 与 `docs/superpowers/plans/`。
