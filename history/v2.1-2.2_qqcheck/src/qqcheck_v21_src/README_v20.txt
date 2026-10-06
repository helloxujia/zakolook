============================================================
 qqcheck v2.0 — QQ 本地只读查询工具（原生二进制版）
============================================================
本目录内容：
  qqcheck         ⭐ 主程序（arm64 原生可执行文件，无需Python）
  qqcheck-full    完整版副本（带SQL统计）
  qqcheck-lite    轻量版（无SQL依赖，备用）
  qqcheck-armv7   32位ARM版（老设备）
  qqcheck.py       Python 版源码（旧版）
  run_py.sh        Python版启动器
  tmp/             读取数据库时的临时副本目录

发行版（可分发传播）：
  /sdcard/Download/qqcheck_bin_20260911/
    qqcheck_arm64 / qqcheck_armv7 / src/ / README.txt / LICENSE.txt

用法: ./qqcheck <命令> ...   详见 README.txt（发行版目录）
安全: 全程离线只读，不联网、不修改数据。
