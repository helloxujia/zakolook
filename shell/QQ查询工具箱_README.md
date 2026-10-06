# QQ 查询工具箱 v1

基于 **ZakoQLook (zql)** 的 shell 交互界面。

```
分层：zql         引擎（17 命令、退出码语义、零解析）→ 给脚本
      本工具箱     界面（菜单交互、批量、美化输出）    → 给人
      工具箱只调用 zql，不重复实现判定逻辑
```

---

## 一、部署

```bash
# 前置：zql 已就位
cp zql /data/local/tmp/zql && chmod 755 /data/local/tmp/zql

# 部署工具箱（建议放 /data 下，/sdcard 权限受限）
cp QQ查询工具箱.sh /data/local/tmp/
chmod 755 /data/local/tmp/QQ查询工具箱.sh

# 运行
su -c "sh /data/local/tmp/QQ查询工具箱.sh"
```

依赖：**root**（QQ 数据在应用私有目录）+ **zql** 可执行文件。

---

## 二、两种用法

### ① 交互菜单（给人）

```bash
su -c "sh /data/local/tmp/QQ查询工具箱.sh"
```

```
  QQ 查询工具箱  · ZakoQLook v1.1
  ────────────────────────────────────────────────
   [1] 本人信息        QQ号 / 昵称 / 签名 / 等级
   [2] 群验证          输入群号 → 权威判定在否
   [3] 群列表          已加入群（数量 / 明细）
   [4] 群详情          群名 / 成员数 / 我的头衔等级
   [5] 群成员          按群内等级排序（含头衔）
   [6] QQ 资料         输入 QQ号 → 昵称 / 等级
   [7] 消息检索        指定群 + 关键词（范围受限）
   [8] 批量验证        多群号逐个判定
   [9] 环境自检        账号 / 缓存 / 依赖
   [0] 退出
```

### ② 命令透传（给脚本）

任何参数原样交给 zql，**退出码语义不变**：

```bash
sh /data/local/tmp/QQ查询工具箱.sh self-qq              # → 100000001
sh /data/local/tmp/QQ查询工具箱.sh in-group 123456789   # → true   (exit 0)
sh /data/local/tmp/QQ查询工具箱.sh in-group 234567890  # → false  (exit 1)
sh /data/local/tmp/QQ查询工具箱.sh groups --json | jq
sh /data/local/tmp/QQ查询工具箱.sh msgs 123456789 --kw 工具箱 --n 5
```

---

## 三、菜单项做什么

| 项 | 调用 | 说明 |
|---|---|---|
| [1] | `zql self` + `self-level` | 本人 QQ号/昵称/签名/性别/UID + 等级 |
| [2] | `zql in-group` ×N | **权威判定**（改自 group_list，已退群不再误判）；支持一次输入多个群号 |
| [3] | `zql groups` / `group-count` | 已加入群数量 + 明细（按人数降序）|
| [4] | `group-name` + `group-title` + `group-level` + `group-members` | 群名 / 我的头衔 / 我的等级 / 成员数 |
| [5] | `zql group-members --n N` | 群成员表（QQ号 / 昵称 / **头衔** / 等级 / 积分）|
| [6] | `zql user` + `user-level` | 指定 QQ 的昵称/签名/等级 |
| [7] | `zql msgs --kw` | **指定群**消息检索（强制限定本人已加入的群）|
| [8] | `zql in-group` 批量 | 粘贴多群号逐个判定 + 统计"在群 N / 不在 M" |
| [9] | `zql status` | 账号空间 / 缓存 / 权限自检 |

---

## 四、退出码（透传模式）

```
0  成功 / 判定为真（在群）      1  判定为假（不在群）
2  用法错误                     3  环境错误（无 root / 未装 zql）
4  数据不可用
```

---

## 五、排错

| 现象 | 原因 | 处置 |
|---|---|---|
| `✗ 未找到 zql 可执行文件` | zql 未部署 | `cp zql /data/local/tmp/ && chmod 755` |
| 同上 | zql 在别处 | `ZQL=/path/to/zql sh QQ查询工具箱.sh` |
| `✗ 需要 root` | 非 root | `su -c "sh …"` |
| `Permission denied` | 脚本在 /sdcard 且权限不足 | 复制到 `/data/local/tmp/` 再跑 |
| 菜单显示乱码方块 | 终端不支持该字符 | 功能不受影响 |

---

## 六、与 zql 的关系

```
· 不重复实现：判定/查询全部走 zql，接口变更只需改 zql
· 透传模式：脚本可完全绕开菜单，直接当 zql 用
· 退出码语义一致：透传模式下与 zql 完全相同
```
