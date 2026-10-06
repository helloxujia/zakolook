#!/system/bin/sh
# ════════════════════════════════════════════════════════════════
#  QQ 查询工具箱 v1 · 基于 ZakoQLook (zql)
#    定位: zql=引擎(给脚本) / 本工具=界面(给人)
#    特性: 菜单交互 + 命令透传双模 · 退出码语义 · 范围受限检索
#    依赖: zql 二进制(默认 /data/local/tmp/zql)
#    用法: sh QQ查询工具箱.sh            → 交互菜单
#          sh QQ查询工具箱.sh verify 123  → 直接执行(透传 zql)
# ════════════════════════════════════════════════════════════════

# ── 配色 ──
G=$(printf '\033[32m'); R=$(printf '\033[31m'); Y=$(printf '\033[33m')
C=$(printf '\033[36m'); B=$(printf '\033[1m');  D=$(printf '\033[90m'); N=$(printf '\033[0m')

# ── zql 定位 ──
ZQL="${ZQL:-}"
if [ -z "$ZQL" ]; then
    for p in /data/local/tmp/zql /system/bin/zql /data/adb/zql; do
        [ -x "$p" ] && { ZQL="$p"; break; }
    done
fi

line() { printf '  %s%s%s\n' "$D" "────────────────────────────────────────────────────" "$N"; }
ok()   { printf '  %s✓%s %s\n' "$G" "$N" "$1"; }
bad()  { printf '  %s✗%s %s\n' "$R" "$N" "$1"; }
warn() { printf '  %s!%s %s\n' "$Y" "$N" "$1"; }
info() { printf '  %s·%s %s\n' "$C" "$N" "$1"; }
title(){ printf '\n%s%s%s\n' "$B$C" "$1" "$N"; line; }

# ── 前置检查 ──
precheck() {
    if [ ! -x "$ZQL" ]; then
        bad "未找到 zql 可执行文件"
        info "请先部署: cp zql /data/local/tmp/zql && chmod 755 /data/local/tmp/zql"
        info "或用环境变量指定: ZQL=/path/to/zql sh $0"
        return 1
    fi
    if [ "$(id -u 2>/dev/null)" != "0" ]; then
        bad "需要 root（QQ 数据在应用私有目录）"
        info "请用: su -c \"sh $0\""
        return 1
    fi
    return 0
}

# ── 命令透传（有参数时）──
if [ $# -gt 0 ]; then
    precheck || exit 3
    exec "$ZQL" "$@"
fi

# ══════════════ 功能项 ══════════════

m_self() {
    title "本人信息"
    "$ZQL" self 2>&1 | sed 's/^/  /'
    printf '  %s年龄:%s ' "$D" "$N"; "$ZQL" self-age 2>/dev/null || printf '?'
    printf '\n'
    printf '  %s注：QQ 账号等级（客户端显示的 Lv.NN）为服务端数据，本地库不含%s\n' "$D" "$N"
    pause
}

m_verify() {
    title "群验证（权威判定）"
    printf '  输入群号（可多个，空格分隔）: '
    read -r INPUT
    [ -z "$INPUT" ] && { warn "未输入"; pause; return; }
    line
    for g in $INPUT; do
        "$ZQL" in-group "$g" >/dev/null 2>&1
        rc=$?
        if [ $rc -eq 0 ]; then
            nm=$("$ZQL" group-name "$g" 2>/dev/null)
            printf '  %s%s%s  %s在群%s  %s\n' "$B" "$g" "$N" "$G" "$N" "$nm"
        else
            case $rc in
                3) printf '  %s%s%s  %s环境错误%s\n' "$B" "$g" "$N" "$R" "$N" ;;
                2) printf '  %s%s%s  %s参数错误%s\n' "$B" "$g" "$N" "$Y" "$N" ;;
                4) printf '  %s%s%s  %s数据不可用%s\n' "$B" "$g" "$N" "$Y" "$N" ;;
                *) printf '  %s%s%s  %s不在群%s\n' "$B" "$g" "$N" "$R" "$N" ;;
            esac
        fi
    done
    pause
}

m_groups() {
    title "已加入群"
    n=$("$ZQL" group-count 2>/dev/null)
    info "共 ${G}${n}${N} 个群"
    line
    "$ZQL" groups 2>/dev/null | sed 's/^/  /'
    pause
}

m_group_detail() {
    title "群详情"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    if ! "$ZQL" in-group "$g" >/dev/null 2>&1; then
        bad "不在该群（或群号错误）"
        pause; return
    fi
    nm=$("$ZQL" group-name "$g" 2>/dev/null)
    myqq=$("$ZQL" self-qq 2>/dev/null)
    t=$("$ZQL" group-title "$g" "$myqq" 2>/dev/null)
    lv=$("$ZQL" group-level "$g" "$myqq" 2>/dev/null)
    # 成员数：取服务端声明（group_list.60006，准确；缓存计数含退群残留会偏多）
    cnt=$("$ZQL" groups 2>/dev/null | awk -F'\t' -v G="$g" '$1==G{print $3}' | head -1)
    # 我的角色
    myrole="成员"
    own=$("$ZQL" group-owner "$g" 2>/dev/null | cut -f1)
    [ "$own" = "$myqq" ] && myrole="群主"
    if [ "$myrole" = "成员" ]; then
        "$ZQL" group-admins "$g" 2>/dev/null | cut -f1 | grep -qx "$myqq" && myrole="管理员"
    fi
    line
    printf '  群名    : %s\n' "$nm"
    printf '  我的角色: %s\n' "$myrole"
    printf '  我的等级: %s\n' "${lv:-?}"
    printf '  我的头衔: %s\n' "${t:--}"
    printf '  成员数  : %s\n' "${cnt:-?}"
    printf '  群号    : %s\n' "$g"
    pause
  }

m_members() {
    title "群成员（按群内等级降序）"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    printf '  显示条数（默认 20）: '
    read -r n
    [ -z "$n" ] && n=20
    line
    printf '  %-12s %-16s %-8s %-8s %-6s %s\n' "QQ号" "昵称" "角色" "头衔" "等级" "积分"
    "$ZQL" group-members "$g" --n "$n" 2>/dev/null | while IFS="$(printf '\t')" read -r q nm rl t lv p; do
        [ -z "$nm" ] && nm="-"
        [ -z "$rl" ] && rl="-"
        [ -z "$t" ] && t="-"
        printf '  %-12s %-16s %-8s %-8s %-6s %s\n' "$q" "$nm" "$rl" "$t" "$lv" "$p"
    done
    pause
}

m_roles() {
    title "群角色（群主 / 管理员）"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    line
    printf '  %-12s %-18s %-8s %s\n' "QQ号" "昵称" "角色" "等级"
    "$ZQL" group-roles "$g" 2>/dev/null | while IFS="$(printf '\t')" read -r q nm rl lv; do
        [ -z "$nm" ] && nm="-"
        [ -z "$rl" ] && rl="-"
        printf '  %-12s %-18s %-8s %s\n' "$q" "$nm" "$rl" "$lv"
    done
    pause
}

m_levelrank() {
    title "群等级榜（按群等级降序）"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    printf '  显示条数（默认 20）: '
    read -r n
    [ -z "$n" ] && n=20
    line
    printf '  %-4s %-12s %-16s %-7s %-8s %s\n' "#" "QQ号" "昵称" "等级" "角色" "头衔"
    "$ZQL" group-members "$g" --n "$n" 2>/dev/null \
      | awk -F'\t' '{printf "  %-4d %-12s %-16s %-7s %-8s %s\n", NR, $1, substr($2,1,15), $5, ($3==""?"-":$3), ($4==""?"-":$4)}'
    pause
}

m_member_info() {
    title "成员群资料"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    printf '  输入 QQ号（直接回车 = 我自己）: '
    read -r q
    [ -z "$q" ] && q=$("$ZQL" self-qq 2>/dev/null)
    [ -z "$q" ] && { bad "无法确定 QQ 号"; pause; return; }
    lv=$("$ZQL" group-level "$g" "$q" 2>/dev/null)
    t=$("$ZQL" group-title "$g" "$q" 2>/dev/null)
    role="成员"
    own=$("$ZQL" group-owner "$g" 2>/dev/null | cut -f1)
    [ "$own" = "$q" ] && role="群主"
    if [ "$role" = "成员" ]; then
        "$ZQL" group-admins "$g" 2>/dev/null | cut -f1 | grep -qx "$q" && role="管理员"
    fi
    line
    printf '  群号  : %s\n' "$g"
    printf '  QQ号  : %s\n' "$q"
    printf '  角色  : %s\n' "$role"
    printf '  等级  : %s\n' "${lv:-?}"
    printf '  头衔  : %s\n' "${t:--}"
    pause
}

m_my_groups() {
    title "我的群记录（我在各群的记录）"
    printf '  筛选角色（回车=全部；可填 群主/管理员/成员）: '
    read -r rl
    line
    printf '  %-12s %-22s %-7s %-14s %-6s %s\n' "群号" "群名" "角色" "我的群昵称" "等级" "我的头衔"
    if [ -n "$rl" ]; then
        "$ZQL" self-groups --role "$rl" 2>/dev/null
    else
        "$ZQL" self-groups 2>/dev/null
    fi | awk -F'\t' '{printf "  %-12s %-22s %-7s %-14s %-6s %s\n", $1, substr($2,1,21), $3, substr($4,1,13), $5, ($6==""?"-":$6)}'
    printf '\n  输入群号可看该群完整群名，直接回车返回: '
    read -r g
    if [ -n "$g" ]; then
        nm=$("$ZQL" group-name "$g" 2>/dev/null)
        [ -n "$nm" ] && printf '  群名: %s\n' "$nm"
    fi
    pause
}

m_my_summary() {
    title "我的群画像"
    "$ZQL" self-summary 2>&1 | sed 's/^/  /'
    pause
}

m_user() {
    title "QQ 资料查询"
    printf '  输入 QQ 号: '
    read -r q
    [ -z "$q" ] && { warn "未输入"; pause; return; }
    line
    "$ZQL" user "$q" 2>/dev/null | sed 's/^/  /' || bad "未找到该 QQ 的资料"
    printf '  年龄: '
    "$ZQL" user-age "$q" 2>/dev/null || printf '?'
    printf '\n'
    pause
}

m_msgs() {
    title "消息检索（仅限本人已加入的群）"
    printf '  输入群号: '
    read -r g
    [ -z "$g" ] && { warn "未输入"; pause; return; }
    printf '  关键词（回车=只看最近）: '
    read -r kw
    printf '  条数（默认 10）: '
    read -r n
    [ -z "$n" ] && n=10
    line
    if [ -n "$kw" ]; then
        "$ZQL" msgs "$g" --kw "$kw" --n "$n" 2>/dev/null | while IFS='	' read -r t nick kind txt; do
            printf '  [%s] %s <%s>\n      %s\n' "$t" "$nick" "$kind" "$txt"
        done
    else
        "$ZQL" msgs "$g" --n "$n" 2>/dev/null | while IFS='	' read -r t nick kind txt; do
            printf '  [%s] <%s> %s\n' "$t" "$kind" "$txt"
        done
    fi
    pause
}

m_batch() {
    title "批量验证"
    info "每行一个群号（输入完按 Ctrl-D 或空行结束）"
    line
    n=0; y=0
    while read -r g; do
        [ -z "$g" ] && break
        n=$((n+1))
        if "$ZQL" in-group "$g" >/dev/null 2>&1; then
            y=$((y+1))
            printf '  %s✓%s %-12s 在群  %s\n' "$G" "$N" "$g" "$("$ZQL" group-name "$g" 2>/dev/null)"
        else
            printf '  %s✗%s %-12s 不在群\n' "$R" "$N" "$g"
        fi
    done
    line
    printf '  合计 %s 个，其中 %s在群 %d%s / %s不在 %d%s\n' "$n" "$G" "$y" "$N" "$R" "$((n-y))" "$N"
    pause
}

m_env() {
    title "环境自检"
    line
    "$ZQL" status 2>&1 | sed 's/^/  /'
    printf '\n'
    [ -x "$ZQL" ] && ok "zql: $ZQL" || bad "zql 未找到"
    [ "$(id -u 2>/dev/null)" = "0" ] && ok "权限: root" || bad "权限: 非 root"
    du -sh /data/local/tmp/zql_data/cache 2>/dev/null | awk '{printf "  缓存: %s\n", $1}'
    pause
}

# ══════════════ QQ 频道 ══════════════

m_guilds() {
    title "我加入的 QQ 频道"
    n=$("$ZQL" guild-list 2>/dev/null | wc -l)
    info "共 ${G}${n}${N} 个频道"
    line
    "$ZQL" guild-list 2>/dev/null | while IFS="$(printf '\t')" read -r id nm; do
        printf '  %s%-20s%s %s\n' "$D" "$id" "$N" "$nm"
    done
    pause
}

# 频道定位：ID / 短名 / 名称 都行；留空则用 $ZQL_GUILD
_gpick() {
    printf '\n  输入频道（ID / 短名 / 名称，留空用默认频道）: '
    read -r _g
    if [ -z "$_g" ]; then
        if [ -n "${ZQL_GUILD:-}" ]; then
            _g="$ZQL_GUILD"
        else
            warn "未输入，且未设 ZQL_GUILD"
            pause; return 1
        fi
    fi
    if ! "$ZQL" in-guild "$_g" >/dev/null 2>&1; then
        bad "未加入该频道，或定位不到: $_g"
        pause; return 1
    fi
    return 0
}

m_gverify() {
    title "频道验证（权威判定）"
    printf '  输入频道（可多个，空格分隔；支持 ID / 短名 / 名称）: '
    read -r INPUT
    [ -z "$INPUT" ] && { warn "未输入"; pause; return; }
    line
    for g in $INPUT; do
        "$ZQL" in-guild "$g" >/dev/null 2>&1
        rc=$?
        if [ $rc -eq 0 ]; then
            nm=$("$ZQL" guild-info "$g" 2>/dev/null | awk -F' : ' '/^名称/{print $2}')
            printf '  %s%-22s%s %s已加入%s  %s\n' "$B" "$g" "$N" "$G" "$N" "$nm"
        else
            case $rc in
                3) printf '  %s%-22s%s %s环境错误%s\n' "$B" "$g" "$N" "$R" "$N" ;;
                2) printf '  %s%-22s%s %s参数错误%s\n' "$B" "$g" "$N" "$Y" "$N" ;;
                4) printf '  %s%-22s%s %s数据不可用%s\n' "$B" "$g" "$N" "$Y" "$N" ;;
                *) printf '  %s%-22s%s %s未加入（或仅浏览过）%s\n' "$B" "$g" "$N" "$R" "$N" ;;
            esac
        fi
    done
    pause
}

m_gdetail() {
    title "频道详情"
    _gpick || return
    line
    "$ZQL" guild-info "$_g" 2>/dev/null | sed 's/^/  /'
    pause
}

m_gchannels() {
    title "子频道（按分组）"
    _gpick || return
    line
    "$ZQL" guild-channels "$_g" 2>/dev/null | while IFS="$(printf '\t')" read -r a b c; do
        if [ -z "$b" ]; then
            printf '\n  %s%s%s\n' "$C" "$a" "$N"
        else
            printf '    %s%-20s%s %-28s %s%s%s\n' "$D" "$a" "$N" "$b" "$Y" "$c" "$N"
        fi
    done
    pause
}

m_groles() {
    title "身份组"
    _gpick || return
    line
    "$ZQL" guild-roles "$_g" 2>/dev/null | while IFS="$(printf '\t')" read -r id nm tag col cnt; do
        if [ -n "$col" ]; then
            printf '  %s%-4s%s %-22s %-8s %s%-7s%s %s\n' "$D" "$id" "$N" "$nm" "${tag:--}" "$C" "$col" "$N" "$cnt"
        else
            printf '  %s%-4s%s %-22s %s%-7s%s %s\n' "$D" "$id" "$N" "$nm" "$C" "$tag" "$N" "$cnt"
        fi
    done
    pause
}

m_gmsgs() {
    title "频道消息（实验性）"
    warn "guild_msg.db 有部分页仅在 WAL 且未解开，结果可能取不全"
    _gpick || return
    printf '  关键词（可留空）: '
    read -r kw
    printf '  条数（默认 20）: '
    read -r n
    [ -z "$n" ] && n=20
    line
    if [ -n "$kw" ]; then
        "$ZQL" guild-msgs "$_g" --exp --kw "$kw" --n "$n" 2>/dev/null | while IFS="$(printf '\t')" read -r t nick txt; do
            printf '  [%s] %s%s%s %s\n' "$t" "$C" "$nick" "$N" "$txt"
        done
    else
        "$ZQL" guild-msgs "$_g" --exp --n "$n" 2>/dev/null | while IFS="$(printf '\t')" read -r t nick txt; do
            printf '  [%s] %s%s%s %s\n' "$t" "$C" "$nick" "$N" "$txt"
        done
    fi
    pause
}

pause() {
    printf '\n  %s按回车返回菜单...%s' "$D" "$N"
    read -r _ 2>/dev/null || true
}

# ══════════════ 主菜单 ══════════════

menu() {
    while :; do
        clear 2>/dev/null
        printf '\n'
        printf '  %sQQ 查询工具箱%s  %s· ZakoQLook v1.5.1%s\n' "$B$C" "$N" "$D" "$N"
        printf '  %s────────────────────────────────────────────────%s\n' "$D" "$N"
        printf '\n'
        printf '   %s[1]%s 本人信息        QQ号 / 昵称 / 签名 / 年龄\n' "$C" "$N"
        printf '   %s[2]%s 群验证          输入群号 → 权威判定在否\n' "$C" "$N"
        printf '   %s[3]%s 群列表          已加入群（数量 / 明细）\n' "$C" "$N"
        printf '   %s[4]%s 群详情          我的角色等级 / 群名 / 成员数\n' "$C" "$N"
        printf '   %s[5]%s 群成员          按群内等级排序（含头衔）\n' "$C" "$N"
        printf '   %s[6]%s QQ 资料         输入 QQ号 → 昵称 / 年龄\n' "$C" "$N"
        printf '   %s[7]%s 消息检索        指定群 + 关键词（范围受限）\n' "$C" "$N"
        printf '   %s[8]%s 批量验证        多群号逐个判定\n' "$C" "$N"
        printf '   %s[9]%s 环境自检        账号 / 缓存 / 依赖\n' "$C" "$N"
        printf '   %s[10]%s 群角色         群主 / 管理员列表\n' "$C" "$N"
        printf '   %s[11]%s 群等级榜       按群等级降序（Top N）\n' "$C" "$N"
        printf '   %s[12]%s 成员资料       查某人的角色 / 等级 / 头衔\n' "$C" "$N"
        printf '   %s[13]%s 我的群记录     我在各群的角色 / 等级 / 群昵称\n' "$C" "$N"
        printf '   %s[14]%s 我的群画像     群数 / 角色分布 / 等级分布\n' "$C" "$N"
        printf '   %s[15]%s 我的频道        我加入的 QQ 频道\n' "$C" "$N"
        printf '   %s[16]%s 频道验证        输入频道 → 是否已加入\n' "$C" "$N"
        printf '   %s[17]%s 频道详情        子频道数 / 身份组 / 我的等级\n' "$C" "$N"
        printf '   %s[18]%s 子频道          按分组列出（含类型）\n' "$C" "$N"
        printf '   %s[19]%s 身份组          名称 / 标签 / 颜色 / 人数\n' "$C" "$N"
        printf '   %s[20]%s 频道消息        实验性（可能取不全）\n' "$C" "$N"
        printf '   %s[0]%s 退出\n' "$C" "$N"
        printf '\n'
        printf '   %s用法提示:%s 也可直接调用  sh %s verify 123456\n' "$D" "$N" "$(basename "$0")"
        printf '\n   %s选择:%s ' "$B" "$N"
        read -r ch || exit 0
        case "$ch" in
            1) m_self ;;
            2) m_verify ;;
            3) m_groups ;;
            4) m_group_detail ;;
            5) m_members ;;
            6) m_user ;;
            7) m_msgs ;;
            8) m_batch ;;
            9) m_env ;;
            10) m_roles ;;
            11) m_levelrank ;;
            12) m_member_info ;;
            13) m_my_groups ;;
            14) m_my_summary ;;
            15) m_guilds ;;
            16) m_gverify ;;
            17) m_gdetail ;;
            18) m_gchannels ;;
            19) m_groles ;;
            20) m_gmsgs ;;
            0|q|Q|exit) printf '\n  再见。\n\n'; exit 0 ;;
            *) ;;
        esac
    done
}

if precheck; then
    menu
else
    printf '\n'
    exit 3
fi
