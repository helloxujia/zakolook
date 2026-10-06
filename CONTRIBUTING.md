# 参与贡献

感谢愿意帮忙。本文很短，请花两分钟看完。

## 一、提交前签名（DCO）

本项目采用 **DCO**（Developer Certificate of Origin），不要求签 CLA。
提交时加 `-s` 即可：

```bash
git commit -s -m "fix: 修正 xxx"
```

会在提交信息末尾生成：

```
Signed-off-by: 你的名字 <你的邮箱>
```

含义：你确认这段代码是你写的、你有权提交、并以本项目许可（AGPL-3.0-or-later）授权出去。

> 注意：用了 DCO 之后，含你贡献的部分**不能被项目方闭源**。
> 如果你不接受这一点，请不要提交 —— 这点必须提前说清。

## 二、环境搭建

```bash
# 需要 Go 1.21+（本机实测 1.27.1 / linux-arm64）
git clone https://gitee.com/xujia2024/zakolook（镜像 / Mirror: https://github.com/helloxujia/zakolook）.git
cd zakolook

# 编译（aarch64 静态）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags="-s -w" -o zql .
```

> 依赖目录**不要**放在 fuse 挂载（如 /sdcard）上，会报 `function not implemented`。
> 建议 `GOPATH/GOMODCACHE/GOCACHE` 都设在 `/data/local/tmp` 下。

## 三、测试

**必须在真机跑通**（模拟器读不到 QQ 数据）：

```bash
cp zql /data/local/tmp/zql && chmod 755 /data/local/tmp/zql
su -c "/data/local/tmp/zql status"
su -c "/data/local/tmp/zql guild-list"
```

提交前请附上：

```
· 你的 Android 版本 / 机型 / QQ 版本
· 改动前后的命令输出对比
· 如果涉及解密：解密是否过了页 1 判据（--version 与 status 输出）
```

## 四、代码约定

```
格式       gofmt 必须通过（提交前跑 gofmt -l .，不应有输出）
注释       中文，讲"为什么"而不是"是什么"
判据       任何解密相关改动都必须保留页 1 自检：过不了就报错，绝不吐错数据
日志       stderr 走提示（以 "#" 开头），stdout 只放结果
新增命令   必须同步更新 doc/命令参考.md 与 doc/调用规范.md
版本号     只加功能不升号（分发锚点用 tag 区分）
```

## 五、绝对不要提交

```
× 任何 QQ 数据库文件、缓存、用户数据
× 任何密钥、口令、token、账号信息
× 解出来的明文库（*.plain.db）
× /sdcard 下的临时产物
× 编译器产物（bin/ 目录里的二进制由维护者统一构建发布）
```

`.gitignore` 已经挡掉大部分，但请自己再检查一遍 `git status`。

## 六、Issue / PR 之外

- **安全边界问题**（比如某段代码可能被误用）请优先开 Issue 说明
- **发现某版本 QQ 解不开了** → 非常有价值，请按 Issue 模板提供信息
- **文档改进** → 直接 PR，最受欢迎

## 七、许可

提交即表示你同意以 **AGPL-3.0-or-later**（见 [LICENSE](LICENSE)）分发你的贡献，
并同意 LICENSE 末尾依第 7 条提出的追加条款（署名与来源标注要求）。
