============================================================
 qqcheck v2.0 — QQ 本地数据只读查询工具（ARM 原生可执行文件）
============================================================
全程离线只读：不联网、不修改数据、不上传任何内容。

【文件说明】
  qqcheck_arm64     现代安卓手机（arm64-v8a，绝大多数字机型）
  qqcheck_armv7     较老的 32 位 ARM 设备（armeabi-v7a）
  src/              完整源码（Go 1.27 编译，含 go.mod/go.sum）

【运行要求】
  1) 手机需要 root（KernelSU / Magisk 等）
  2) Android 7.0+；无需安装 Python / Termux

【安装（二选一）】
  A. 直接推送：
     adb push qqcheck_arm64 /data/local/tmp/qqcheck
     adb shell chmod 755 /data/local/tmp/qqcheck
  B. 手机端(root shell)：
     cp /sdcard/Download/qqcheck_bin_20260911/qqcheck_arm64 /data/local/tmp/qqcheck
     chmod 755 /data/local/tmp/qqcheck

【使用（root shell）】
  qqcheck check  <号码>                综合体检（自动判定 群/用户）
  qqcheck group  <群号>                群报告（是否在群+证据+成员统计）
  qqcheck user   <QQ号>                用户报告（映射+互动痕迹+所在群角色）
  qqcheck member <群号> <QQ号|uid>     成员角色（群主/管理员/头衔）
  qqcheck uid    <QQ号>                号码 -> 内部UID
  qqcheck search <关键词>              全位置搜索（含上下文）
  qqcheck overview                     账号总览
  选项:  --json 输出JSON   --space main|dual 限定空间

【示例】
  qqcheck check 123456789
  qqcheck member 123456789 300000002
  qqcheck user 300000002 --json

【安全与合规】
  * 只读设计：不会写入/修改 QQ 任何数据，不发起任何网络请求
  * 仅限本人 / 已获授权的设备与账号使用
  * 禁止用于非法目的；使用者自行承担一切责任
  * 本工具允许自由复制、传播（请保留本说明与许可声明）

【兼容性】Android 7.0+ · arm64 / armv7 · 零依赖单文件
============================================================
