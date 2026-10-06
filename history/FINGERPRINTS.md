# 历史版本指纹核验清单

> 本目录所有历史材料的 MD5，供核对留档是否被改动。

生成：2026-10-05

---

## 一、阶段一 · qqcheck v2.0（2026-09-11）

### 二进制

| 文件 | 字节 | 说明 | MD5 |
|---|---|---|---|
| `bin/qqcheck_arm64` | 7,143,584 | 完整版（arm64）| `168d8e20841e4458b96c44589c67f7c3` |
| `bin/qqcheck_armv7` | 7,143,584 | 完整版（32 位 ARM）| `0a85411c02bd863c677442b4e8a284c6` |
| `bin/qqcheck_lite_arm64` | 2,883,744 | 轻量版（无 SQL 依赖）| `30480fe18ab42bc24c530c05b634dcdd` |

### 源码

| 文件 | 说明 |
|---|---|
| `src/qqcheck.py` | Python 版源码（v1.0 原型，27,337 字节，MD5 `2ee05645cdf240566c824d1268ce180c`）|
| `src/run_py.sh` | Python 版启动器 |
| `src/main.go` | Go 版主源码（v2.0）|
| `src/db_lite.go` | 轻量构建标签（无 SQL）|
| `src/db_sql.go` | 完整构建（含 SQL 统计）|
| `src/go.mod` / `src/go.sum` | Go 依赖（modernc.org/sqlite 等）|

### 文档

| 文件 | 说明 |
|---|---|
| `README.txt` | 原 README |
| `README_发行版.txt` | 发行版说明（含分发目录结构）|
| `LICENSE.txt` | 原许可 |

---

## 二、阶段二 · qqcheck v2.1 / v2.2（2026-10-04）

### 二进制

| 文件 | 字节 | 说明 | MD5 |
|---|---|---|---|
| `bin/qqcheck-lite-v21` | 2,883,744 | v2.1 轻量版（机器友好接口）| `0610e1478c18131f75a7df359618c16c` |
| `bin/qqcheck-lite-v22` | 2,883,744 | v2.2 轻量版（+频道识别）| `b644c6b57ccdf61624c5b1aa46e89775` |
| `bin/qqcheck-full-v22` | 7,143,584 | v2.2 完整版 | `9344e4d04035855991dea36988c1b185` |

### 源码

```
src/qqcheck_v21_src/     v2.1 源码（main.go 34,304 字节）+ main_v22.go + CHANGELOG
src/qqcheck_src/         v2.0 发行版源码副本（main.go 32,403 字节，与阶段一相同）
```

### 文档

```
CHANGELOG.md             变更记录（v2.1 / v2.2 两节）
```

---

## 三、现版本（非历史，另见仓库根目录）

```
bin/zql                  6b450612bd930782d71948c25aebc013   （v1.1）
shell/QQ查询工具箱.sh      d25d5232b84201871b7c7865878991a4
```

主清单见仓库根目录 [FINGERPRINTS.md](../FINGERPRINTS.md)。

---

## 四、说明

```
· 历史二进制均已固化，可用于行为对照
  （例如：同一群号在 v2.0 / v2.2 / 现版下的判定差异）
· 历史版本不再维护；如需复现，请以本目录材料为准
· 各阶段许可归属见 v2.0_qqcheck/LICENSE.txt 及仓库根 LICENSE
```
