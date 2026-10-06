# qqcheck 变更记录

## v2.2 (2026-10-04)

### 新增：号码类型区分（频道 / 群 / QQ号）

`check <号码>` 现可判定三类，依据两条独立线索：

1. 位数区间（不依赖数据，恒定可用）
   实测本机 3 空间频道缓存样本 70 条，得区间：
   - 5~12 位   群号 / QQ号
   - 16~18 位  频道号（guild_id）
   - 13~15、19~20 位  未观测，归为 unknown，不臆断

2. 数据域痕迹
   - 频道：`guild_mmkv_configurations*` 中出现 `"str_guild_id":"<号>"` 或 `"guild_id":<号>`
   - 群  ：成员头衔缓存 `<号>-u_xxx` 键 / `TroopFileTansferItemEntity<号>` 表
   - 用户：号码→UID 映射 / 聊天列表 / 互动标识

### 新增 `ChannelResult` 输出

```json
{
  "channel": "1000000000000006",
  "kind": "channel",
  "id_length": 18,
  "in_channel": true,
  "evidence": ["主空间：频道缓存中出现该编号"],
  "note": "本地频道数据仅含浏览缓存，不能证明已加入该频道"
}
```

注意：本地频道数据源仅 feed 浏览缓存，**证据强度弱于群**，
因此不提供 `in_group` 语义，字段名用 `in_channel`，并固定输出免责说明。

### 变更

- `normNumber` 位数上限由 12 放宽至 20（容纳频道号）。
- 报错文案同步更新：`应为 5~20 位数字`。

### 兼容性

- 5~12 位号码行为不变。
- 文本报告、JSON 既有字段不变。
- 新增类型仅在 `check` 命令生效；`group` 仍按群处理。

### 产物

```
qqcheck-lite-v22  2883744 字节  md5 b644c6b57ccdf61624c5b1aa46e89775
qqcheck-full-v22  7143584 字节  md5 9344e4d04035855991dea36988c1b185
```

### 待办

- [ ] 频道详情报告（成员/帖子统计，视可用数据源而定）
- [ ] 内联包重新生成（当前内联版本仍为 v2.0）
- [ ] JSON schema 文档化

---

## v2.1 (2026-10-04)

### 新增：机器友好判定接口

原版仅提供人类可读报告与 JSON 数据，调用方只能解析 verdict 自然语言措辞。
verdict 措辞随证据强度变化（四档），措辞一变调用方即失效。

本版新增三种机器接口：

1. JSON 顶层结构化字段（`group --json`）
   - `in_group`   bool   是否在群（best_score > 0）
   - `best_score` int    跨空间所有账号的最高证据分
   - `confidence` string high(>=6) / medium(>=3) / weak(>0) / none(0)

2. `--in-group` 布尔模式
   只输出 true / false，退出码 0。

3. `--strict-exit` 退出码语义
   0 = 在群，1 = 不在群，2 = 错误。

### 修复

- 未知子命令原返回 exit=0（调用方误判成功），现返回 exit=2。
- `check` 命令一并支持上述接口：
  - `check <群号> --in-group` / `--strict-exit` 与 `group` 行为一致
  - `check <用户号> --in-group` 报错退出（群语义不适用），exit=2

### 实现说明

群报告的输出与退出码收敛到单一函数 `emitGroupOutcome()`，
`group` 与 `check`（群情形）共用，避免逻辑分叉。

### 产物校验（本版最终）

```
qqcheck-lite-v21  2883744 字节  md5 0610e1478c18131f75a7df359618c16c
qqcheck-full-v21  7143584 字节  md5 02bd974f0e70e70f79e914aa2c40e651
```

### 兼容性

- 文本模式输出逐字节不变。
- JSON 模式仅新增字段，原有字段与结构不变。
- 原有命令与选项行为不变。

### 构建

```
export GOROOT=/data/local/tmp/go-toolchain/go
export GOPATH=/data/local/tmp/gopath
export GOCACHE=/data/local/tmp/gocache
export GOFLAGS=-mod=mod GOTOOLCHAIN=local CGO_ENABLED=0

go build -tags sql  -ldflags "-s -w" -o qqcheck-full-v21 .   # 完整版
go build -tags lite -ldflags "-s -w" -o qqcheck-lite-v21 .   # 轻量版
```

产物校验（aarch64）：
```
qqcheck-lite-v21  2883744 字节  md5 358792dbb5df3aa3f07119eac30049d6
qqcheck-full-v21  7143584 字节  md5 8c300000010b72ab332e5a34106c352
```

### 部署

```
/data/local/tmp/qqcheck/qqcheck-lite-v21
/data/local/tmp/qqcheck/qqcheck-full-v21
```

原件保留于 `/sdcard/_zako_backup/qqcheck_<时间戳>/`。

### 判定档位说明（源码 src/main.go:490）

| score | verdict | 含义 |
|---|---|---|
| >=6 | 已加入该群（强证据） | 在群 |
| 3~5 | 在该群（中等证据） | 在群 |
| 1~2 | 仅弱痕迹 | 在群 |
| 0 | 未发现证据 | 不在群 |

调用方不应解析 verdict 文本，应使用 `--in-group` 或读 `in_group` 字段。

### 待办

- [ ] 群内昵称纳入 user 报告输出
- [ ] JSON schema 文档化
- [ ] 错误码细分（参数 / 未找到 / 权限）
