#!/system/bin/sh
# qqcheck 启动器：自动定位 python3，非 root 时自动尝试 su
DIR=$(cd "$(dirname "$0")" && pwd)
PY=""
for p in "$(command -v python3 2>/dev/null)" /data/data/com.termux/files/usr/bin/python3 /system/bin/python3; do
  [ -n "$p" ] && [ -x "$p" ] && PY="$p" && break
done
if [ -z "$PY" ]; then
  echo "[qqcheck] 未找到 python3。可从 Termux 运行，或指定 python3 路径。"
  exit 1
fi
if [ "$(id -u)" != "0" ]; then
  echo "[qqcheck] 提示: 需要 root 权限，尝试通过 su 提权..."
  exec su -c "'$PY' '$DIR/qqcheck.py' $*"
fi
exec "$PY" "$DIR/qqcheck.py" "$@"
