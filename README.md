<p align="center">
  <img src="mascot.png" width="140" alt="ZakoQLook mascot" />
</p>

<h1 align="center">ZakoQLook (zql)</h1>

<p align="center"><b>QQ 本地数据查询工具 —— 只读、离线、shell 友好</b></p>

不登录、不联网、不改数据。直接读你手机上 QQ 自己的数据库，
把结果吐成一行行文本或 JSON，方便写脚本、做自动化。

> **适用前提：本机、本人账号。**
> 注意：**严禁用于黑灰产、信息窃取等任何非法用途** —— 详见 [DISCLAIMER.md](DISCLAIMER.md)（合法使用声明与免责条款）。

```
$ su -c "zql guild-list"
1000000000000004  示例频道A
1000000000000003 示例频道B
...
# 我加入的频道 42 个（另有 9 个仅浏览过）[主空间]

$ su -c "zql in-group 123456789"     # → true   （exit 0）
$ su -c "zql guild-info sample-channel" # → 频道详情
```

---

## 一、30 秒上手

```bash
# 1) 放到可执行位置（/sdcard 是 fuse，不能直接执行）
cp bin/zql /data/local/tmp/zql && chmod 755 /data/local/tmp/zql

# 2) 自检（应列出账号空间）
su -c "/data/local/tmp/zql status"

# 3) 开始用
su -c "/data/local/tmp/zql self"          # 本人资料
su -c "/data/local/tmp/zql in-group 123456"  # 是否在群
su -c "/data/local/tmp/zql guild-list"    # 我加入的 QQ 频道
```

需要 **root**（读应用私有目录），这是硬前提。

## 二、能查什么

**群（22 条命令）**

```
status                    环境与账号自检
self / self-qq / name / sign / age      本人资料
in-group <群号>           权威判定 → true/false（exit 0/1）
group-name / groups / group-count       群名 / 群列表 / 数量
group-title / group-level / group-members
group-owner / group-admins / group-roles   群主 / 管理员 / 角色总览
user / user-name / user-age             指定 QQ 的资料
self-groups / self-summary              我在各群的记录 / 群画像
accounts                                列出账号：空间 / QQ号 / 昵称 / 活动-历史
  --qq <QQ号>                           全局选项：定向到指定账号（多账号/分身场景）
msgs <群号> [--kw 词]     群消息检索（限本人已加入的群）
```

**QQ 频道（6 条命令）**

```
guild-list [--all]        我加入的频道
in-guild <ID|短名|名称>   是否已加入 → true/false
guild-info               频道详情（子频道数 / 身份组 / 我的身份组）
guild-channels           子频道（按分组）
guild-roles              身份组（名称 / 标签 / 颜色 / 人数）
guild-msgs [--exp]       频道消息检索（实验性）
```

支持环境变量 `ZQL_GUILD` 设默认频道，之后可省略参数：

```bash
export ZQL_GUILD=sample-channel
zql guild-info            # 等价于 zql guild-info sample-channel
```

完整说明见 [doc/命令参考.md](doc/命令参考.md) 与 [doc/调用规范.md](doc/调用规范.md)。

## 三、设计取向

```
只读      永远不修改原库，只解到缓存目录
离线      不联网、不注入、不使用任何协议端（OneBot/NapCat 等）
shell 友好 退出码表达语义 · 提示走 stderr · 列表制表符分隔 · --json 可编程
自检优先  解密后先验证页首判据，过不了就明确报错 —— 绝不吐出解密不全的数据
可逆      改前备份、打 tag；每个版本附 FINGERPRINTS md5 清单
```

退出码约定：

```
0 成功 / 判定为真     1 判定为假     2 用法错     3 环境错     4 数据不可用
```

## 四、它是怎么工作的

一句话：QQ 的本地库是 SQLCipher 变体（自定义 1024 字节私有头 + 4096 页），
本项目在本地把口令推导出来、把页解开，然后当普通 SQLite 只读查询。

- nt 系库（消息/群/资料…）：`口令 = md5(md5(uid) + 库头 rand)`
- ORM 系库（gpro 频道库）：`口令 = md5( md5(库头载荷) + md5( md5(QQ号) + md5(sha1_raw(QQ号)) ) )`

推导过程、参数、判据与已知缺口，见 **[doc/频道库解密原理.md](doc/频道库解密原理.md)**。

## 五、文档

| 文件 | 内容 |
|---|---|
| [doc/命令参考.md](doc/命令参考.md) | 28 条命令逐条说明 |
| [doc/调用规范.md](doc/调用规范.md) | 脚本怎么调（含频道脚本模板） |
| [doc/频道库解密原理.md](doc/频道库解密原理.md) | 频道库解密全套原理与判据 |
| [doc/原理与实现.md](doc/原理与实现.md) | 整体实现说明 |
| [doc/退出码与输出约定.md](doc/退出码与输出约定.md) | 退出码与输出格式契约 |
| [art/](art/) | 文化艺术图（中秋一册 · 八幅） |
| [ROADMAP.md](ROADMAP.md) | 待办与方向 |
| [DISCLOSURE.md](DISCLOSURE.md) | 示例数据与真实标识说明 |
| [DISCLAIMER.md](DISCLAIMER.md) | 合法使用声明与免责条款 |

## 六、文化艺术图

中秋一册。主题角色 **夏川 · 澪**，即本项目的主题吉祥物（与作者的其它项目共用同一形象）。

> 页首所示即为吉祥物图（`mascot.png`）。美术作品著作权归作者所有，
> **不适用本仓库的 AGPL-3.0**。

<p align="center">
  <a href="art/index.html"><img src="art/thumbs/01-zako-laugh.jpg" width="150" alt="杂鱼~ 哈哈…"></a>
  <a href="art/index.html"><img src="art/thumbs/02-mooncake-give.jpg" width="150" alt="月饼给你~"></a>
  <a href="art/index.html"><img src="art/thumbs/04-huahaoyueyuan.jpg" width="150" alt="花好月圆"></a>
  <a href="art/index.html"><img src="art/thumbs/07-moonlit-night.jpg" width="150" alt="月夜"></a>
</p>

<p align="center">
  <b><a href="art/index.html">打开图册</a></b>　·　八幅　·　
  <a href="https://xiaozayu.top/%E5%A4%8F%E5%B7%9D.%E6%BE%AA/">关于她</a>
</p>

> 美术作品著作权归作者所有，**不适用本仓库的 AGPL-3.0**。详见 [art/README.md](art/README.md)。

---

## 七、参与

欢迎 Issue 与 PR，规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。
提交请用 `git commit -s`（DCO 签名）。

## 八、许可

**AGPL-3.0-or-later** —— 全文见 [LICENSE](LICENSE)。

LICENSE 末尾另含 **依第 7 条提出的追加条款**：

```
· 分发或通过网络提供本作品时，必须显著声明原项目仓库：
      https://gitee.com/xujia2024/zakolook
      https://github.com/helloxujia/zakolook
· 必须完整保留版权声明与 NOTICE / AUTHORS
· 不得暗示衍生作品由原作者创作或背书
· 修改版须显著标记修改内容与日期
· 项目名称与标识未经许可不得用于衍生作品或商业推广（见 TRADEMARK.md）
```

通俗说：**代码随便用、随便改、随便商用** —— 但改了要开源、必须署名、
且**不能把名字拿去混淆**。想拿它做收费的在线服务？AGPL 第 13 条要求你
把服务端源码也开放。

> **关于历史文档中的真实标识**：见 [DISCLOSURE.md](DISCLOSURE.md) ——
> 早期版本示例中作者本人的标识，系**有意保留的真实测试数据**。

第三方来源声明见 [NOTICE](NOTICE)，作者见 [AUTHORS](AUTHORS)，
名称与标识规则见 [TRADEMARK.md](TRADEMARK.md)，
许可全文与第 7 条追加条款见 [LICENSE](LICENSE)。

> 以上为通俗说明；条款效力以 [LICENSE](LICENSE) 原文为准。

---

**版本**：**v1.5.1**
**仓库**
```
Gitee   https://gitee.com/xujia2024/zakolook
GitHub  https://github.com/helloxujia/zakolook   （镜像）
```
两处内容保持一致；Issue / PR 在任一平台提出都可以。
**字体/依赖**：Go 标准库 + `modernc.org/sqlite`
