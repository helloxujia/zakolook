// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// ZakoQLook (zql) —— QQ 本地数据查询代理（shell 友好）
//
// 设计目标：
//   1. 子命令即查询，结果直接可被 shell 捕获
//   2. 退出码表达语义，调用方无需解析文本
//   3. 解密结果带缓存，重复调用不重复解密
//
// 退出码：
//   0 成功 / 判定为真        1 判定为假（不在群等）
//   2 用法错误               3 环境错误（无 root / 无数据目录）
//   4 数据不可用（解密失败 / 字段缺失）

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	version    = "1.5.1"
	authorName = "xujia2024"
	repoURL    = "https://gitee.com/xujia2024/zakolook"
	repoMirror = "https://github.com/helloxujia/zakolook"
	licenseTag = "AGPL-3.0-or-later"
	appDir     = "/data/local/tmp/zql_data"
	cacheDir   = appDir + "/cache"
	qqBaseFmt  = "/data/user/%s/com.tencent.mobileqq"
	ntDBSub    = "databases/nt_db"
)

var (
	optJSON  bool
	optSpace = "all"
	optFresh bool
	optQuiet bool
	optAll   bool
	optWAL   bool
	optExp   bool
)

// ---------- 退出码 ----------
const (
	exitOK    = 0
	exitFalse = 1
	exitUsage = 2
	exitEnv   = 3
	exitData  = 4
)

func die(code int, msg string) {
	if msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(code)
}

func out(s string) {
	if !optQuiet {
		fmt.Println(s)
	}
}

// ---------- 空间与库定位 ----------

type acct struct {
	Space   string // 主空间 / 分身(u998) ...
	UserID  string // 0 / 998 / 999
	QQ      string // 本账号 QQ 号
	UID     string // u_xxxx
	AcctDir string // nt_qq_<md5>
	BaseDir string // .../databases/nt_db/nt_qq_<md5>
	PkgDir  string // .../com.tencent.mobileqq
}

func detectAccounts() []acct {
	var res []acct
	users := []struct{ id, name string }{
		{"0", "主空间"}, {"998", "分身(u998)"}, {"999", "分身(u999)"},
	}
	for _, u := range users {
		base := fmt.Sprintf(qqBaseFmt, u.id)
		if !isDir(base) {
			continue
		}
		ntRoot := filepath.Join(base, ntDBSub)
		ents, err := os.ReadDir(ntRoot)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "nt_qq_") {
				continue
			}
			a := acct{
				Space: u.name, UserID: u.id,
				AcctDir: e.Name(),
				BaseDir: filepath.Join(ntRoot, e.Name()),
				PkgDir:  base,
			}
			a.QQ, a.UID = resolveIdentity(a)
			res = append(res, a)
		}
	}
	return res
}

// resolveIdentity 读取 QQ 自存的映射文件（明文，无需解密）
//
//	<pkg>/files/uid/<QQ号>###<uid>
//
// 该文件同时给出本账号的 QQ 号与 uid，且是明文，无需解密即可读取，
// 也不依赖内存读取或 login.db。
func resolveIdentity(a acct) (qq, uid string) {
	uidDir := filepath.Join(a.PkgDir, "files", "uid")
	ents, err := os.ReadDir(uidDir)
	if err == nil {
		for _, e := range ents {
			n := e.Name()
			if i := strings.Index(n, "###"); i > 0 {
				q := strings.TrimSpace(n[:i])
				u := strings.TrimSpace(n[i+3:])
				if strings.HasPrefix(u, "u_") {
					// 多账号时取目录名匹配的那个
					if uid == "" {
						qq, uid = q, u
					}
				}
			}
		}
	}
	if uid != "" {
		writeUIDCache(a, uid)
		return qq, uid
	}
	// 回落：files/user/u_<QQ>_t
	uDir := filepath.Join(a.PkgDir, "files", "user")
	if es, err := os.ReadDir(uDir); err == nil {
		for _, e := range es {
			n := e.Name()
			if strings.HasPrefix(n, "u_") {
				rest := strings.TrimPrefix(n, "u_")
				if i := strings.Index(rest, "_"); i > 0 {
					qq = rest[:i]
					uid = readUIDCache(a)
					if uid == "" {
						uid = "u_" + rest
					}
					return qq, uid
				}
			}
		}
	}
	return "", readUIDCache(a)
}

func uidCachePath(a acct) string {
	return filepath.Join(cacheDir, "u"+a.UserID+"_"+strings.TrimPrefix(a.AcctDir, "nt_qq_")+".uid")
}

func readUIDCache(a acct) string {
	b, err := os.ReadFile(uidCachePath(a))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeUIDCache(a acct, uid string) {
	os.MkdirAll(cacheDir, 0700)
	os.WriteFile(uidCachePath(a), []byte(uid), 0600)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---------- 解密与缓存 ----------

// dbFile 返回加密库路径
func (a acct) dbFile(name string) string {
	return filepath.Join(a.BaseDir, name)
}

// plainDB 返回明文库路径（必要时解密）
func (a acct) plainDB(name string) (string, error) {
	src := a.dbFile(name)
	st, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("库不存在: %s", name)
	}
	dst := filepath.Join(cacheDir, "u"+a.UserID+"_"+strings.TrimSuffix(name, ".db")+".plain.db")
	meta := dst + ".meta"

	key := fmt.Sprintf("%d:%d", st.Size(), st.ModTime().Unix())
	if optWAL {
		if ws, e := os.Stat(src + "-wal"); e == nil {
			key += fmt.Sprintf("|w%d:%d", ws.Size(), ws.ModTime().Unix())
		}
	}
	if !optFresh {
		if b, err := os.ReadFile(meta); err == nil && strings.TrimSpace(string(b)) == key {
			if _, err := os.Stat(dst); err == nil {
				return dst, nil
			}
		}
	}
	// 逐库取 rand（各库可不同）
	if a.UID == "" {
		a.UID = readUIDCache(a)
	}
	if a.UID == "" {
		return "", fmt.Errorf("未能确定 uid（需先解密任一库以获取），可尝试: zql status")
	}
	os.MkdirAll(cacheDir, 0700)
	info, err := decryptFileAuto(src, dst, a.UID, a.QQ)
	if err != nil {
		return "", fmt.Errorf("解密失败 %s: %v", name, err)
	}
	// WAL 合并默认关闭。
	// 实测：QQ 在运行时主库本身自洽可查；强行并入 -wal 反而会 malformed
	//（WAL 里有未提交/跨检查点的帧）。需要时用 --wal 显式开启。
	if optWAL {
		if wal := src + "-wal"; fileExists(wal) {
			res := 48
			if v, e := strconv.Atoi(info["reserve"]); e == nil && v > 0 {
				res = v
			}
			if _, e := mergeWALFile(dst, wal, src, info["pass"], res); e != nil && !optQuiet {
				fmt.Fprintf(os.Stderr, "# 提示: WAL 未合并（%v）\n", e)
			}
		}
	}
	os.WriteFile(meta, []byte(key), 0600)
	// 解密成功后顺便回填 uid
	if a.UID != "" {
		writeUIDCache(a, a.UID)
	}
	return dst, nil
}

// openDB 打开明文库
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	return db, nil
}

// ---------- 数据查询 ----------

type SelfInfo struct {
	Space  string `json:"space"`
	UID    string `json:"uid"`
	QQ     string `json:"qq"`
	Name   string `json:"name"`
	Sign   string `json:"sign"`
	Gender string `json:"gender"`
}

var genderMap = map[string]string{"1": "男", "2": "女", "0": "未设置"}

func querySelf(a acct) (*SelfInfo, error) {
	p, err := a.plainDB("profile_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT "1002","20002","20011","20014","1000" FROM profile_info_v6 LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("无资料记录")
	}
	var qq, name, sign, gender, uid sql.NullString
	if err := rows.Scan(&qq, &name, &sign, &gender, &uid); err != nil {
		return nil, err
	}
	s := &SelfInfo{Space: a.Space, UID: uid.String, QQ: qq.String, Name: name.String, Sign: sign.String}
	s.Gender = genderMap[gender.String]
	if a.UID == "" && uid.String != "" {
		a.UID = uid.String
		writeUIDCache(a, uid.String)
	}
	return s, nil
}

// inGroup 权威判定：群号是否在 group_list
func inGroup(a acct, code string) (bool, string, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return false, "", err
	}
	db, err := openDB(p)
	if err != nil {
		return false, "", err
	}
	defer db.Close()
	var name sql.NullString
	err = db.QueryRow(`SELECT "60007" FROM group_list WHERE "60001"=?`, code).Scan(&name)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, name.String, nil
}

type GroupRow struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Members int64  `json:"members"`
}

func listGroups(a acct) ([]GroupRow, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT "60001","60007","60006" FROM group_list`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []GroupRow
	for rows.Next() {
		var g GroupRow
		var code, name sql.NullString
		var mem sql.NullInt64
		if err := rows.Scan(&code, &name, &mem); err != nil {
			continue
		}
		g.Code, g.Name, g.Members = code.String, name.String, mem.Int64
		res = append(res, g)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Members > res[j].Members })
	return res, nil
}

type UserInfo struct {
	QQ   string `json:"qq"`
	Name string `json:"name"`
	Sign string `json:"sign"`
}

func queryUser(a acct, qq string) (*UserInfo, error) {
	p, err := a.plainDB("profile_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var name, sign sql.NullString
	err = db.QueryRow(`SELECT "20002","20011" FROM profile_info_v6 WHERE "1002"=? LIMIT 1`, qq).Scan(&name, &sign)
	if err == sql.ErrNoRows {
		// 回落：群成员表
		gp, gerr := a.plainDB("group_info.db")
		if gerr == nil {
			if gdb, gerr2 := openDB(gp); gerr2 == nil {
				defer gdb.Close()
				var n2 sql.NullString
				if e := gdb.QueryRow(`SELECT "20002" FROM group_member3 WHERE "1002"=? LIMIT 1`, qq).Scan(&n2); e == nil {
					return &UserInfo{QQ: qq, Name: n2.String}, nil
				}
			}
		}
		return nil, fmt.Errorf("未找到该 QQ 的资料")
	}
	if err != nil {
		return nil, err
	}
	return &UserInfo{QQ: qq, Name: name.String, Sign: sign.String}, nil
}

// ---------- 输出 ----------

func emitJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		die(exitData, "JSON 序列化失败: "+err.Error())
	}
	fmt.Println(string(b))
}

func usage() {
	fmt.Print(`ZakoQLook (zql) —— QQ 本地数据查询代理 v` + version + `

用法: zql <命令> [参数] [选项]

命令:
  status                     环境与账号自检
  self                       本人资料（QQ号 · 昵称 · 签名 · 性别 · 年龄）
  self-qq                    仅输出本人 QQ 号
  self-name                  仅输出本人昵称
  self-sign                  仅输出本人签名
  self-age                   本人年龄（本地资料 24103）
  self-groups [--role R]     我在各群的记录（群名 / 角色 / 群昵称 / 等级 / 头衔）
                             --role 群主|管理员|成员  可筛选
  self-summary               我的群画像（群数 / 角色分布 / 等级分布 / 入群跨度）
  in-group <群号>            是否在群（权威判定）→ 输出 true/false
  group-name <群号>          群名（不在群则退出码 1）
  groups [--count]           已加入的群列表（群号/群名/人数）
  group-count                已加入群数量
  user <QQ号>                指定 QQ 的资料
  user-name <QQ号>           仅输出昵称
  user-age <QQ号>            指定 QQ 的年龄（本地资料）
  group-level <群号> <QQ号>  群等级（64035，与 QQ 客户端显示的 LV 一致）
  group-title <群号> <QQ号>  群头衔（无自定义头衔时回落为角色）
  group-owner <群号>         群主（64017=1，每群有且仅有 1 人）
  group-admins <群号>        管理员列表（64017=2）
  group-roles <群号>         群主 + 管理员总览
  group-members <群号>       群成员（QQ/昵称/角色/头衔/等级/积分），可加 --n N
  msgs <群号> [--kw 词]      指定群消息检索（仅限本人已加入的群）
                            可加 --n N / --days D

  ── QQ 频道（gpro 库，ORM 系加密）──
  guild-list [--all]         我加入的频道（频道ID / 名称）
                             --all 同时列出仅浏览过、未加入的
  in-guild <ID|短名|名称>    是否已加入该频道 → 输出 true/false
  guild-info <ID|短名|名称>  频道详情（子频道数 / 身份组数 / 我的身份组）
  guild-channels <ID|短名|名称>
                             子频道列表（按分组，含类型与 ID）
  guild-roles <ID|短名|名称> 身份组列表（名称 / 标签 / 颜色 / 人数）

  ※ 频道类命令可设默认频道，设过之后参数可省略：
      export ZQL_GUILD=1000000000000001     # 或短名 / 名称
      zql guild-info          # 等价于 zql guild-info 1000000000000001


  ※ groups 与 self-groups 的区别：
     groups      = 群清单（群号 / 群名 / 人数）
     self-groups = 我在各群的记录（我的群昵称 / 角色 / 等级）

实验性功能，需显式加 --exp 才可用；结果可能不完整，不建议作为唯一依据
  guild-msgs <频道ID> [--kw 词] [--n N]
                             频道消息检索（限本人已加入的频道）
                             短板：guild_msg.db 的部分页仅存在于 -wal 中，而
                             SQLCipher 的 WAL 帧加密方式与主库页不同（尚未解出），
                             因此可能取不全

已废弃
  self-level / user-level    本地库不含 QQ 账号等级（服务端数据）
                             改用: self-age / user-age 查年龄

选项:
  --json                     结构化输出
  --space all|main|dual      限定账号空间（默认 all）
  --fresh                    强制重新解密（忽略缓存）
  --wal                      解密时并入 -wal（默认不并；QQ 运行中并入易致 malformed）
  --exp                      启用实验性命令（见上方实验性一节）
  --quiet                    抑制非结果输出
  --all                      对所有空间输出（默认取首个）

退出码:
  0  成功 / 判定为真（在群）
  1  判定为假（不在群）
  2  用法错误
  3  环境错误（无 root / 未发现 QQ 数据目录）
  4  数据不可用（解密失败 / 字段缺失）

示例:
  zql self-qq                          # → 100000001
  zql self-age                         # → 16   （年龄）
  zql self-groups                      # 我在各群的记录（角色/群昵称/等级）
  zql self-groups --role 群主           # 只看我当群主的群
  zql self-summary                     # 我的群画像
  zql self-level                       # → 16
  zql group-title 123456789 100000001 # → 示例头衔
  zql guild-list                       # 我加入的 QQ 频道
  zql in-guild 1000000000000002       # → true（示例频道C）
  zql group-members 123456789 --n 5    # 群内等级前 5
  zql group-owner 123456789            # → 100000001  示例昵称  群主
  zql group-admins 123456789           # 管理员列表
  zql group-roles 123456789            # 群主 + 管理员总览
  zql msgs 123456789 --kw 关键词 --n 10 # 指定群检索
  zql in-group 234567890 && echo 在群  # 退出码判定，零解析
  zql groups --count                   # → 85
  zql user-name 123456 && echo ok
`)
}

// ---------- 主流程 ----------

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage()
		os.Exit(exitOK)
	}
	var rest []string
	var bad []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			optJSON = true
		case a == "--fresh":
			optFresh = true
		case a == "--quiet":
			optQuiet = true
		case a == "--all":
			optAll = true
		case a == "--wal":
			optWAL = true
		case a == "--exp":
			optExp = true
		case a == "--count":
			rest = append(rest, "--count")
		case a == "--n" || a == "--kw" || a == "--days" || a == "--role":
			// 命令专属选项：原样传递给子命令（含取值）
			rest = append(rest, a)
			if i+1 < len(args) {
				rest = append(rest, args[i+1])
				i++
			}
		case a == "--space":
			if i+1 >= len(args) {
				die(exitUsage, "--space 缺少取值（all|main|dual）")
			}
			optSpace = args[i+1]
			i++
		case strings.HasPrefix(a, "--space="):
			optSpace = strings.SplitN(a, "=", 2)[1]
		case a == "-v" || a == "--version":
			fmt.Println("ZakoQLook (zql) v" + version)
			fmt.Println("Project : " + repoURL)
			fmt.Println("Mirror  : " + repoMirror)
			fmt.Println("License : " + licenseTag)
			fmt.Println("Copyright (c) 2026 " + authorName)
			fmt.Println()
			fmt.Println("分发或通过网络提供本作品时，须显著声明原项目仓库地址，见 LICENSE 第 7 节。")
			os.Exit(exitOK)
		case strings.HasPrefix(a, "-"):
			bad = append(bad, a)
		default:
			rest = append(rest, a)
		}
	}
	if len(bad) > 0 {
		die(exitUsage, "未知选项: "+strings.Join(bad, " "))
	}
	switch optSpace {
	case "all", "main", "dual":
	default:
		die(exitUsage, "--space 取值无效: "+optSpace+"（可用: all|main|dual）")
	}

	if os.Geteuid() != 0 {
		die(exitEnv, "[错误] 需要 root 读取 QQ 数据目录。请用: su -c \"zql ...\"")
	}
	if len(rest) == 0 {
		usage()
		os.Exit(exitUsage)
	}
	cmd := rest[0]
	accts := detectAccounts()
	switch optSpace {
	case "main":
		accts = filterAccts(accts, true)
	case "dual":
		accts = filterAccts(accts, false)
	}
	if len(accts) == 0 {
		if cmd == "status" {
			die(exitEnv, "未发现 QQ 数据目录（QQ 未安装或未登录过）")
		}
		die(exitEnv, "未发现 QQ 数据目录")
	}

	switch cmd {
	case "status":
		cmdStatus(accts)
	case "self", "self-qq", "self-name", "self-sign":
		cmdSelf(accts, cmd)
	case "in-group":
		cmdInGroup(accts, rest)
	case "group-name":
		cmdGroupName(accts, rest)
	case "groups":
		cmdGroups(accts, rest)
	case "group-count":
		cmdGroupCount(accts)
	case "user", "user-name":
		cmdUser(accts, rest, cmd)
	case "self-level":
		cmdSelfLevel(accts)
	case "self-age":
		cmdSelfAge(accts)
	case "self-groups":
		cmdSelfGroups(accts, rest)
	case "self-summary":
		cmdSelfSummary(accts)
	case "user-level":
		cmdUserLevel(accts, rest)
	case "user-age":
		cmdUserAge(accts, rest)
	case "group-level":
		cmdGroupLevel(accts, rest)
	case "group-members":
		cmdGroupMembers(accts, rest)
	case "group-title":
		cmdGroupTitle(accts, rest)
	case "group-owner":
		cmdGroupOwner(accts, rest)
	case "group-admins":
		cmdGroupAdmins(accts, rest)
	case "group-roles":
		cmdGroupRoles(accts, rest)
	case "msgs":
		cmdMsgs(accts, rest)
	case "guild-list":
		cmdGuildList(accts, rest)
	case "in-guild":
		cmdInGuild(accts, rest)
	case "guild-info":
		cmdGuildInfo(accts, rest)
	case "guild-channels":
		cmdGuildChannels(accts, rest)
	case "guild-roles":
		cmdGuildRoles(accts, rest)
	case "guild-msgs":
		if !optExp {
			die(exitUsage, "guild-msgs 为实验性功能，需显式加 --exp。\n"+
				"短板：guild_msg.db 的部分页仅存在于 -wal，而 SQLCipher 的 WAL 帧加密方式与主库页不同（尚未解出），结果可能取不全。")
		}
		cmdGuildMsgs(accts, rest)
	default:
		die(exitUsage, "未知命令: "+cmd+"\n运行 zql --help 查看用法")
	}
}

func filterAccts(as []acct, mainOnly bool) []acct {
	var r []acct
	for _, a := range as {
		if (a.Space == "主空间") == mainOnly {
			r = append(r, a)
		}
	}
	return r
}

func pick(accts []acct) acct {
	if optAll {
		return accts[0]
	}
	return accts[0]
}

func cmdStatus(accts []acct) {
	type st struct {
		Space   string `json:"space"`
		UserID  string `json:"user_id"`
		Account string `json:"account_dir"`
		UID     string `json:"uid"`
		Libs    int    `json:"lib_count"`
	}
	var list []st
	for _, a := range accts {
		ents, _ := os.ReadDir(a.BaseDir)
		n := 0
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".db") {
				n++
			}
		}
		list = append(list, st{a.Space, a.UserID, a.AcctDir, a.UID, n})
	}
	if optJSON {
		emitJSON(map[string]any{"version": version, "accounts": list, "cache": cacheDir})
		return
	}
	out("ZakoQLook v" + version)
	out("缓存目录: " + cacheDir)
	for _, s := range list {
		out(fmt.Sprintf("  %-14s uid=%-26s 库=%d  %s", s.Space, s.UID, s.Libs, s.Account))
	}
	out(fmt.Sprintf("共 %d 个账号空间", len(list)))
}

func cmdSelf(accts []acct, cmd string) {
	a := pick(accts)
	info, err := querySelf(a)
	if err != nil {
		die(exitData, "读取资料失败: "+err.Error())
	}
	switch cmd {
	case "self-qq":
		fmt.Println(info.QQ)
	case "self-name":
		fmt.Println(info.Name)
	case "self-sign":
		fmt.Println(info.Sign)
	default:
		if optJSON {
			emitJSON(info)
			return
		}
		out("QQ号: " + info.QQ)
		out("昵称: " + info.Name)
		out("签名: " + info.Sign)
		out("性别: " + info.Gender)
		out("UID:  " + info.UID)
	}
}

func cmdInGroup(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql in-group <群号>")
	}
	code := rest[1]
	if _, err := strconv.ParseUint(code, 10, 64); err != nil {
		die(exitUsage, "群号格式不正确: "+code)
	}
	// 遍历所有空间，任一处命中即判定在群（多账号友好）
	found, name, space := false, "", ""
	for _, a := range accts {
		ok, n, err := inGroup(a, code)
		if err != nil {
			continue
		}
		if ok {
			found, name, space = true, n, a.Space
			break
		}
	}
	if optJSON {
		emitJSON(map[string]any{
			"group": code, "in_group": found, "name": name, "space": space,
		})
	} else if found {
		fmt.Println("true")
		if !optQuiet && name != "" {
			fmt.Fprintf(os.Stderr, "# %s  [%s]\n", name, space)
		}
	} else {
		fmt.Println("false")
	}
	if found {
		os.Exit(exitOK)
	}
	os.Exit(exitFalse)
}

func cmdGroupName(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql group-name <群号>")
	}
	code := rest[1]
	for _, a := range accts {
		ok, name, err := inGroup(a, code)
		if err == nil && ok {
			fmt.Println(name)
			os.Exit(exitOK)
		}
	}
	die(exitFalse, "不在该群或未找到: "+code)
}

func cmdGroups(accts []acct, rest []string) {
	countOnly := false
	for _, r := range rest {
		if r == "--count" {
			countOnly = true
		}
	}
	a := pick(accts)
	gs, err := listGroups(a)
	if err != nil {
		die(exitData, "读取群列表失败: "+err.Error())
	}
	if countOnly {
		fmt.Println(len(gs))
		return
	}
	if optJSON {
		emitJSON(gs)
		return
	}
	for _, g := range gs {
		out(fmt.Sprintf("%s\t%s\t%d人", g.Code, g.Name, g.Members))
	}
	if !optQuiet {
		fmt.Fprintf(os.Stderr, "# 共 %d 个群\n", len(gs))
	}
}

func cmdGroupCount(accts []acct) {
	a := pick(accts)
	gs, err := listGroups(a)
	if err != nil {
		die(exitData, "读取群列表失败: "+err.Error())
	}
	fmt.Println(len(gs))
}

// ---------- 等级与消息（v1.1 扩展） ----------

// qqAge 本地资料中的"年龄"（profile_info_v6.24103）
//
// 该字段是年龄，不是 QQ 等级（v1.4 修正）。
//
//	验证：出生年 + 24103 ≈ 当前年份，14/14 吻合
//	  （如 2009 年生 → 16；1934 年生 → 92）
//
// QQ 账号等级（客户端显示的 Lv.NN）为服务端数据，本地库不含。
func qqAge(a acct, qq string) (int, bool, error) {
	p, err := a.plainDB("profile_info.db")
	if err != nil {
		return 0, false, err
	}
	db, err := openDB(p)
	if err != nil {
		return 0, false, err
	}
	defer db.Close()
	var lv sql.NullInt64
	err = db.QueryRow(`SELECT "24103" FROM profile_info_v6 WHERE "1002"=? LIMIT 1`, qq).Scan(&lv)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return int(lv.Int64), lv.Valid && lv.Int64 > 0, nil
}

// qqLevel 本人/他人 QQ 等级（服务端数据）
//
// 本地库不含 QQ 账号等级（v1.4 修正）。
//
//	旧实现误用 24103（实为年龄）充当等级，已拆分为 qqAge。
//	QQ 等级需联网从服务端获取，本工具（纯离线）无法提供。
func qqLevel(a acct, qq string) (int, bool, error) {
	return 0, false, fmt.Errorf("本地库不含 QQ 账号等级（服务端数据）；可用: zql self-age 查年龄")
}

// GroupMember 群成员（含群内等级与角色）
//
// 角色字段：group_member3."64017"
//
//	1 = 群主（每群有且仅有 1 人，已实测 64 个群全部吻合）
//	2 = 管理员
//	0 = 普通成员
//
// 旧实现用 "64027"==1 判断管理员，实测有误
//
//	（该字段在群内占比 3%~13%，群主/管理员/普通成员都有取值为 1 的情况）
type GroupMember struct {
	QQ     string `json:"qq"`
	UID    string `json:"uid"`
	Name   string `json:"name"`    // QQ 昵称（20002）
	Card   string `json:"card"`    // 群昵称/名片（64003）
	Title  string `json:"title"`   // 群头衔（64023）
	Level  int64  `json:"level"`   // 群等级，字段 64035，与 QQ 客户端显示的 LV 一致
	Active int64  `json:"active"`  // 群活跃分级（64020，值域较小，非显示等级）
	Points int64  `json:"points"`  // 群内积分（64021）
	Role   int    `json:"role"`    // 群角色（64017）: 1=群主 2=管理员 0=成员
	Owner  bool   `json:"owner"`   // 是否群主
	Admin  bool   `json:"admin"`   // 是否管理员
	JoinTs int64  `json:"join_ts"` // 入群时间（64007）
}

// roleLabel 群角色文字标签（依据 64017）
func roleLabel(m GroupMember) string {
	switch m.Role {
	case 1:
		return "群主"
	case 2:
		return "管理员"
	}
	return "-"
}

// setRole 由 64017 值设置角色标记
func (m *GroupMember) setRole(role sql.NullInt64) {
	m.Role = int(role.Int64)
	m.Owner = m.Role == 1
	m.Admin = m.Role == 2
}

// groupMemberInfo 指定成员在某群的等级/积分
func groupMemberInfo(a acct, group, qq string) (*GroupMember, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var m GroupMember
	var name, card, title, uid sql.NullString
	var lv, act, pts, jt, role sql.NullInt64
	err = db.QueryRow(`SELECT "20002","64003","64023","1000","64035","64020","64021","64007","64017"
		FROM group_member3 WHERE "60001"=? AND "1002"=? LIMIT 1`,
		group, qq).Scan(&name, &card, &title, &uid, &lv, &act, &pts, &jt, &role)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.QQ, m.UID = qq, uid.String
	m.Name, m.Card, m.Title = strings.TrimSpace(name.String), strings.TrimSpace(card.String), strings.TrimSpace(title.String)
	m.Level, m.Active, m.Points, m.JoinTs = lv.Int64, act.Int64, pts.Int64, jt.Int64
	m.setRole(role)
	return &m, nil
}

// groupMembers 群成员列表（按群内等级降序）
func groupMembers(a acct, group string, limit int) ([]GroupMember, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := `SELECT "1002","1000","20002","64003","64023","64035","64020","64021","64007","64017"
	      FROM group_member3 WHERE "60001"=? ORDER BY "64035" DESC, "64021" DESC`
	if limit > 0 {
		q += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := db.Query(q, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []GroupMember
	for rows.Next() {
		var m GroupMember
		var qq, uid, name, card, title sql.NullString
		var lv, act, pts, jt, role sql.NullInt64
		if err := rows.Scan(&qq, &uid, &name, &card, &title, &lv, &act, &pts, &jt, &role); err != nil {
			continue
		}
		m.QQ, m.UID = qq.String, uid.String
		m.Name, m.Card, m.Title = strings.TrimSpace(name.String), strings.TrimSpace(card.String), strings.TrimSpace(title.String)
		m.Level, m.Active, m.Points, m.JoinTs = lv.Int64, act.Int64, pts.Int64, jt.Int64
		m.setRole(role)
		res = append(res, m)
	}
	return res, nil
}

// groupByRole 按角色筛选群成员
//
//	role: 1=群主 2=管理员
func groupByRole(a acct, group string, role int) ([]GroupMember, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := `SELECT "1002","1000","20002","64003","64023","64035","64020","64021","64007","64017"
	      FROM group_member3 WHERE "60001"=? AND "64017"=?
	      ORDER BY "64035" DESC, "64021" DESC`
	rows, err := db.Query(q, group, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []GroupMember
	for rows.Next() {
		var m GroupMember
		var qq, uid, name, card, title sql.NullString
		var lv, act, pts, jt, rl sql.NullInt64
		if err := rows.Scan(&qq, &uid, &name, &card, &title, &lv, &act, &pts, &jt, &rl); err != nil {
			continue
		}
		m.QQ, m.UID = qq.String, uid.String
		m.Name, m.Card, m.Title = strings.TrimSpace(name.String), strings.TrimSpace(card.String), strings.TrimSpace(title.String)
		m.Level, m.Active, m.Points, m.JoinTs = lv.Int64, act.Int64, pts.Int64, jt.Int64
		m.setRole(rl)
		res = append(res, m)
	}
	return res, nil
}

// MyGroup 我在某个群里的"我的记录"
//
// 与 GroupMember 的区别：
//
//	GroupMember = 群成员（某个群里的所有人）
//	MyGroup     = 我（在各群的记录，只有我）
type MyGroup struct {
	Code   string `json:"code"`      // 群号
	Name   string `json:"name"`      // 群名
	Card   string `json:"card"`      // 我在该群的昵称（64003）
	Title  string `json:"title"`     // 我的群头衔（64023）
	Role   int    `json:"role"`      // 群角色（64017）1=群主 2=管理员 0=成员
	RoleTx string `json:"role_text"` // 角色文字
	Level  int64  `json:"level"`     // 我的群等级（64035）
	JoinTs int64  `json:"join_ts"`   // 入群时间（64007）
}

// myGroups 我在各群的记录（group_member3 中 1002=我 的全部行）
func myGroups(a acct, me string) ([]MyGroup, error) {
	p, err := a.plainDB("group_info.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := `SELECT m."60001", g."60007", m."64003", m."64023", m."64017", m."64035", m."64007"
	      FROM group_member3 m LEFT JOIN group_list g ON g."60001"=m."60001"
	      WHERE m."1002"=? ORDER BY m."64035" DESC`
	rows, err := db.Query(q, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []MyGroup
	for rows.Next() {
		var m MyGroup
		var code, name, card, title sql.NullString
		var role, lv, jt sql.NullInt64
		if err := rows.Scan(&code, &name, &card, &title, &role, &lv, &jt); err != nil {
			continue
		}
		m.Code, m.Name = code.String, name.String
		m.Card, m.Title = strings.TrimSpace(card.String), strings.TrimSpace(title.String)
		m.Role = int(role.Int64)
		switch m.Role {
		case 1:
			m.RoleTx = "群主"
		case 2:
			m.RoleTx = "管理员"
		default:
			m.RoleTx = "成员"
		}
		m.Level, m.JoinTs = lv.Int64, jt.Int64
		res = append(res, m)
	}
	return res, nil
}

// cmdSelfGroups 我在各群的记录
//
// 与 groups 的区别：
//
//	groups      = 已加入的群列表（群号 / 群名 / 人数）—— 群维度
//	self-groups = 我在各群的记录（群昵称 / 角色 / 等级）—— 我维度
func cmdSelfGroups(accts []acct, rest []string) {
	a := pick(accts)
	if a.QQ == "" {
		die(exitData, "未能确定本人 QQ 号")
	}
	ms, err := myGroups(a, a.QQ)
	if err != nil {
		die(exitData, "读取失败: "+err.Error())
	}
	if len(ms) == 0 {
		die(exitData, "未找到我在各群的记录")
	}
	roleFilter := ""
	for i, r := range rest {
		if r == "--role" && i+1 < len(rest) {
			roleFilter = rest[i+1]
		}
	}
	var sel []MyGroup
	for _, m := range ms {
		if roleFilter != "" && m.RoleTx != roleFilter {
			continue
		}
		sel = append(sel, m)
	}
	if optJSON {
		emitJSON(sel)
		return
	}
	for _, m := range sel {
		card, title := m.Card, m.Title
		if card == "" {
			card = "-"
		}
		if title == "" {
			title = "-"
		}
		out(fmt.Sprintf("%s\t%s\t%s\t%s\tLv%d\t%s", m.Code, m.Name, m.RoleTx, card, m.Level, title))
	}
	if !optQuiet {
		fmt.Fprintf(os.Stderr, "# 共 %d 个群（我的记录）\n", len(sel))
	}
}

// cmdSelfSummary 我的群画像（统计汇总）
func cmdSelfSummary(accts []acct) {
	a := pick(accts)
	if a.QQ == "" {
		die(exitData, "未能确定本人 QQ 号")
	}
	ms, err := myGroups(a, a.QQ)
	if err != nil {
		die(exitData, "读取失败: "+err.Error())
	}
	gs, _ := listGroups(a)
	own, adm, mem := 0, 0, 0
	var lvs []int64
	var minT, maxT int64
	for _, m := range ms {
		switch m.Role {
		case 1:
			own++
		case 2:
			adm++
		default:
			mem++
		}
		if m.Level > 0 {
			lvs = append(lvs, m.Level)
		}
		if m.JoinTs > 1000000000 {
			if minT == 0 || m.JoinTs < minT {
				minT = m.JoinTs
			}
			if m.JoinTs > maxT {
				maxT = m.JoinTs
			}
		}
	}
	sort.Slice(lvs, func(i, j int) bool { return lvs[i] > lvs[j] })
	var sum int64
	for _, v := range lvs {
		sum += v
	}
	avg := 0.0
	if len(lvs) > 0 {
		avg = float64(sum) / float64(len(lvs))
	}
	type summary struct {
		QQ       string  `json:"qq"`
		Joined   int     `json:"joined_groups"`
		Recorded int     `json:"recorded_groups"`
		Owner    int     `json:"owner"`
		Admin    int     `json:"admin"`
		Member   int     `json:"member"`
		LvMax    int64   `json:"level_max"`
		LvMin    int64   `json:"level_min"`
		LvAvg    float64 `json:"level_avg"`
		JoinFrom int64   `json:"join_from"`
		JoinTo   int64   `json:"join_to"`
	}
	s := summary{QQ: a.QQ, Joined: len(gs), Recorded: len(ms), Owner: own, Admin: adm, Member: mem}
	if len(lvs) > 0 {
		s.LvMax, s.LvMin, s.LvAvg = lvs[0], lvs[len(lvs)-1], avg
	}
	s.JoinFrom, s.JoinTo = minT, maxT
	if optJSON {
		emitJSON(s)
		return
	}
	out("QQ号       : " + a.QQ)
	out(fmt.Sprintf("已加入群   : %d", s.Joined))
	out(fmt.Sprintf("有我的记录 : %d 个群", s.Recorded))
	out(fmt.Sprintf("角色分布   : 群主 %d / 管理员 %d / 成员 %d", s.Owner, s.Admin, s.Member))
	if len(lvs) > 0 {
		out(fmt.Sprintf("我的群等级 : 最高 %d / 最低 %d / 平均 %.1f", s.LvMax, s.LvMin, s.LvAvg))
		top := 5
		if len(lvs) < top {
			top = len(lvs)
		}
		out(fmt.Sprintf("  前 %d 高  : %v", top, lvs[:top]))
	}
	if s.JoinFrom > 0 {
		out("入群时间   : " + timeFull(s.JoinFrom) + " → " + timeFull(s.JoinTo))
	}
}

// MsgHit 群消息检索结果
type MsgHit struct {
	Ts   int64  `json:"ts"`
	QQ   string `json:"qq"`
	Nick string `json:"nick"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// searchGroupMsgs 在指定群范围内检索消息，不跨群。
//
//	kw 为空则返回最近 N 条
func searchGroupMsgs(a acct, group, kw string, limit int, days int) ([]MsgHit, error) {
	p, err := a.plainDB("nt_msg.db")
	if err != nil {
		return nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT "40050","40033","40093","40011","40800" FROM group_msg_table
	      WHERE "40021"=? `
	args := []any{group}
	if days > 0 {
		cut := timeNow() - int64(days*86400)
		q += `AND "40050">=? `
		args = append(args, cut)
	}
	q += "ORDER BY rowid DESC LIMIT " + strconv.Itoa(limit*8)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []MsgHit
	for rows.Next() {
		var ts, typ sql.NullInt64
		var qq, nick sql.NullString
		var blob []byte
		if err := rows.Scan(&ts, &qq, &nick, &typ, &blob); err != nil {
			continue
		}
		kind, text := decodeBody(blob)
		if kw != "" && !strings.Contains(text, kw) {
			continue
		}
		res = append(res, MsgHit{Ts: ts.Int64, QQ: qq.String, Nick: nick.String, Kind: kind, Text: text})
		if len(res) >= limit {
			break
		}
	}
	return res, nil
}

// cmdSelfAge 本人年龄（本地资料 24103）
func cmdSelfAge(accts []acct) {
	a := pick(accts)
	if a.QQ == "" {
		die(exitData, "未能确定本人 QQ 号")
	}
	age, ok, err := qqAge(a, a.QQ)
	if err != nil {
		die(exitData, "读取失败: "+err.Error())
	}
	if !ok {
		die(exitData, "未获取到年龄")
	}
	if optJSON {
		emitJSON(map[string]any{"qq": a.QQ, "age": age})
		return
	}
	fmt.Println(age)
}

// cmdSelfLevel 已废弃（v1.4）：本地库不含 QQ 账号等级
func cmdSelfLevel(accts []acct) {
	die(exitData, "本地库不含 QQ 账号等级（服务端数据）\n提示: 用 zql self-age 查年龄（本地资料 24103）")
}

// cmdUserAge 指定 QQ 的年龄（本地资料 24103）
func cmdUserAge(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql user-age <QQ号>")
	}
	qq := rest[1]
	for _, a := range accts {
		age, ok, err := qqAge(a, qq)
		if err != nil || !ok {
			continue
		}
		if optJSON {
			emitJSON(map[string]any{"qq": qq, "age": age})
		} else {
			fmt.Println(age)
		}
		os.Exit(exitOK)
	}
	die(exitData, "未获取到该 QQ 的年龄: "+qq)
}

// cmdUserLevel 已废弃（v1.4）：本地库不含 QQ 账号等级
func cmdUserLevel(accts []acct, rest []string) {
	die(exitData, "本地库不含 QQ 账号等级（服务端数据）\n提示: 用 zql user-age <QQ号> 查年龄（本地资料 24103）")
}

func cmdGroupLevel(accts []acct, rest []string) {
	if len(rest) < 3 {
		die(exitUsage, "用法: zql group-level <群号> <QQ号>")
	}
	g, qq := rest[1], rest[2]
	for _, a := range accts {
		m, err := groupMemberInfo(a, g, qq)
		if err != nil || m == nil {
			continue
		}
		if optJSON {
			emitJSON(m)
		} else {
			fmt.Println(m.Level)
		}
		os.Exit(exitOK)
	}
	die(exitData, "未找到该成员（可能不在群或缓存未同步）")
}

func cmdGroupTitle(accts []acct, rest []string) {
	if len(rest) < 3 {
		die(exitUsage, "用法: zql group-title <群号> <QQ号>")
	}
	g, qq := rest[1], rest[2]
	for _, a := range accts {
		m, err := groupMemberInfo(a, g, qq)
		if err != nil || m == nil {
			continue
		}
		title := m.Title
		if title == "" {
			switch m.Role {
			case 1:
				title = "群主"
			case 2:
				title = "管理员"
			default:
				title = "-"
			}
		}
		if optJSON {
			emitJSON(m)
		} else {
			fmt.Println(title)
		}
		os.Exit(exitOK)
	}
	die(exitData, "未找到该成员（可能不在群或缓存未同步）")
}

func cmdGroupMembers(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql group-members <群号> [--n N]")
	}
	g := rest[1]
	limit := 0
	for i, r := range rest {
		if r == "--n" && i+1 < len(rest) {
			limit, _ = strconv.Atoi(rest[i+1])
		}
	}
	a := pick(accts)
	ms, err := groupMembers(a, g, limit)
	if err != nil {
		die(exitData, "读取群成员失败: "+err.Error())
	}
	if len(ms) == 0 {
		die(exitData, "无成员记录（该群可能未同步成员）")
	}
	if optJSON {
		emitJSON(ms)
		return
	}
	for _, m := range ms {
		title := m.Title
		if title == "" {
			title = "-"
		}
		out(fmt.Sprintf("%s\t%s\t%s\t%s\tLv%d\t%d分",
			m.QQ, m.Name, roleLabel(m), title, m.Level, m.Points))
	}
	if !optQuiet {
		fmt.Fprintf(os.Stderr, "# 共 %d 人\n", len(ms))
	}
}

// cmdGroupOwner 群主（64017=1，每群有且仅有 1 人）
func cmdGroupOwner(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql group-owner <群号>")
	}
	g := rest[1]
	if _, err := strconv.ParseUint(g, 10, 64); err != nil {
		die(exitUsage, "群号格式不正确: "+g)
	}
	for _, a := range accts {
		ms, err := groupByRole(a, g, 1)
		if err != nil || len(ms) == 0 {
			continue
		}
		if optJSON {
			emitJSON(ms[0])
			return
		}
		out(fmt.Sprintf("%s\t%s\t群主", ms[0].QQ, ms[0].Name))
		os.Exit(exitOK)
	}
	die(exitData, "未找到该群的群主（群可能未同步成员）")
}

// cmdGroupAdmins 管理员列表（64017=2）
func cmdGroupAdmins(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql group-admins <群号>")
	}
	g := rest[1]
	if _, err := strconv.ParseUint(g, 10, 64); err != nil {
		die(exitUsage, "群号格式不正确: "+g)
	}
	for _, a := range accts {
		ms, err := groupByRole(a, g, 2)
		if err != nil {
			continue
		}
		if len(ms) == 0 {
			// 该空间无记录，尝试下一个空间
			continue
		}
		if optJSON {
			emitJSON(ms)
			return
		}
		for _, m := range ms {
			out(fmt.Sprintf("%s\t%s\t%s\tLv%d", m.QQ, m.Name, roleLabel(m), m.Level))
		}
		if !optQuiet {
			fmt.Fprintf(os.Stderr, "# 共 %d 名管理员\n", len(ms))
		}
		os.Exit(exitOK)
	}
	die(exitData, "未找到该群的管理员（群可能未同步成员）")
}

// cmdGroupRoles 群主与管理员总览
func cmdGroupRoles(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql group-roles <群号>")
	}
	g := rest[1]
	if _, err := strconv.ParseUint(g, 10, 64); err != nil {
		die(exitUsage, "群号格式不正确: "+g)
	}
	for _, a := range accts {
		owners, e1 := groupByRole(a, g, 1)
		admins, e2 := groupByRole(a, g, 2)
		if e1 != nil && e2 != nil {
			continue
		}
		if len(owners) == 0 && len(admins) == 0 {
			continue
		}
		if optJSON {
			emitJSON(map[string]any{"owner": owners, "admins": admins})
			return
		}
		for _, m := range owners {
			out(fmt.Sprintf("%s\t%s\t群主\tLv%d", m.QQ, m.Name, m.Level))
		}
		for _, m := range admins {
			out(fmt.Sprintf("%s\t%s\t管理员\tLv%d", m.QQ, m.Name, m.Level))
		}
		if !optQuiet {
			fmt.Fprintf(os.Stderr, "# 群主 %d 人 / 管理员 %d 人\n", len(owners), len(admins))
		}
		os.Exit(exitOK)
	}
	die(exitData, "未找到该群的角色记录（群可能未同步成员）")
}

func cmdMsgs(accts []acct, rest []string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql msgs <群号> [--kw 关键词] [--n N] [--days D]")
	}
	g := rest[1]
	kw, limit, days := "", 20, 0
	for i := 2; i < len(rest); i++ {
		switch rest[i] {
		case "--kw":
			if i+1 < len(rest) {
				kw = rest[i+1]
				i++
			}
		case "--n":
			if i+1 < len(rest) {
				limit, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--days":
			if i+1 < len(rest) {
				days, _ = strconv.Atoi(rest[i+1])
				i++
			}
		}
	}
	// 群范围强制校验：不在群则拒绝检索（限定范围）
	a := pick(accts)
	if ok, _, err := inGroup(a, g); err == nil && !ok {
		die(exitData, "该群不在已加入列表，检索已限定在本人所在群范围")
	}
	hits, err := searchGroupMsgs(a, g, kw, limit, days)
	if err != nil {
		die(exitData, "检索失败: "+err.Error())
	}
	if len(hits) == 0 {
		die(exitData, "无匹配消息")
	}
	if optJSON {
		emitJSON(hits)
		return
	}
	for _, h := range hits {
		t := timeStr(h.Ts)
		out(fmt.Sprintf("%s\t%s\t%s\t%s", t, h.Nick, h.Kind, h.Text))
	}
}

func cmdUser(accts []acct, rest []string, cmd string) {
	if len(rest) < 2 {
		die(exitUsage, "用法: zql "+cmd+" <QQ号>")
	}
	qq := rest[1]
	if _, err := strconv.ParseUint(qq, 10, 64); err != nil {
		die(exitUsage, "QQ号格式不正确: "+qq)
	}
	var lastErr error
	for _, a := range accts {
		info, err := queryUser(a, qq)
		if err != nil {
			lastErr = err
			continue
		}
		if cmd == "user-name" {
			fmt.Println(info.Name)
		} else if optJSON {
			emitJSON(info)
		} else {
			out("QQ号: " + info.QQ)
			out("昵称: " + info.Name)
			if info.Sign != "" {
				out("签名: " + info.Sign)
			}
		}
		os.Exit(exitOK)
	}
	die(exitData, fmt.Sprintf("查询失败: %v", lastErr))
}
