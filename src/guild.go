// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// QQ 频道（guild）查询。
//
// 「已加入」的判据：t_GPro_MemberRoleListInfo_v1_<guildid> 表存在，
// 且该 guildid 出现在 t_GPro_Guild_v15 中。只满足前者的是退出该频道后
// 残留下来的缓存，不计入。

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Guild struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Short  string `json:"short,omitempty"`
	Joined bool   `json:"joined"`
}

// pbLenFields 极简 protobuf：只收集 length-delimited 字段
func pbLenFields(b []byte) map[int][][]byte {
	m := map[int][][]byte{}
	p := 0
	rv := func() (uint64, bool) {
		var v uint64
		var s uint
		for p < len(b) {
			x := b[p]
			p++
			v |= uint64(x&0x7f) << s
			if x < 0x80 {
				return v, true
			}
			s += 7
			if s > 63 {
				return 0, false
			}
		}
		return 0, false
	}
	for p < len(b) {
		tag, ok := rv()
		if !ok {
			break
		}
		f, w := int(tag>>3), int(tag&7)
		switch w {
		case 0:
			if _, ok := rv(); !ok {
				return m
			}
		case 1:
			p += 8
		case 5:
			p += 4
		case 2:
			l, ok := rv()
			if !ok || p+int(l) > len(b) {
				return m
			}
			m[f] = append(m[f], b[p:p+int(l)])
			p += int(l)
		default:
			return m
		}
	}
	return m
}

func printable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\t') {
			return false
		}
	}
	return true
}

func pbText(f map[int][][]byte, field int) string {
	for _, v := range f[field] {
		s := strings.TrimSpace(string(v))
		if s != "" && printable(s) {
			return s
		}
	}
	return ""
}

// guildArg 取出频道标识：优先使用第 2 个位置参数，该参数缺省或为选项时
// 回落到环境变量 ZQL_GUILD。
func guildArg(rest []string, usg string) string {
	if len(rest) >= 2 && !strings.HasPrefix(rest[1], "-") {
		return rest[1]
	}
	if v := strings.TrimSpace(os.Getenv("ZQL_GUILD")); v != "" {
		return v
	}
	die(exitUsage, "用法: zql "+usg+"\n"+
		"提示: 也可设环境变量作为默认频道，例如\n"+
		"      export ZQL_GUILD=1000000000000001   # 之后 zql guild-info 即可直接调用")
	return ""
}

// guildDBName 定位频道库（gpro_v1-*.db）
func (a acct) guildDBName() (string, error) {
	ents, err := os.ReadDir(a.BaseDir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "gpro_v1") && strings.HasSuffix(n, ".db") {
			return n, nil
		}
	}
	return "", errors.New("未找到频道库 gpro_v1-*.db（可能从未使用过频道功能）")
}

func (a acct) guildPlain() (string, error) {
	name, err := a.guildDBName()
	if err != nil {
		return "", err
	}
	return a.plainDB(name)
}

// loadGuilds 读全部频道 + 是否已加入
func loadGuilds(db *sql.DB) ([]Guild, error) {
	// 先确认这确实是频道库：某些账号空间从未用过频道，库是空壳（只有 1 页）
	var tn string
	if db.QueryRow(`select name from sqlite_master where type='table' and name='t_GPro_Guild_v15'`).Scan(&tn) != nil {
		return nil, errors.New("该账号空间没有频道数据（频道库为空壳 —— 此账号从未使用过 QQ 频道）")
	}
	tabs := map[string]bool{}
	rows, err := db.Query("select name from sqlite_master where type='table'")
	if err != nil {
		return nil, err
	}
	const mrlPfx = "t_GPro_MemberRoleListInfo_v1_"
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			tabs[n] = true
		}
	}
	rows.Close()

	q, err := db.Query(`select guild_id, guild_info from t_GPro_Guild_v15`)
	if err != nil {
		return nil, fmt.Errorf("读取频道表失败: %v", err)
	}
	defer q.Close()
	var list []Guild
	for q.Next() {
		var id int64
		var info []byte
		if q.Scan(&id, &info) != nil {
			continue
		}
		f := pbLenFields(info)
		g := Guild{
			ID:     strconv.FormatInt(id, 10),
			Name:   pbText(f, 8),
			Short:  pbText(f, 27), // 对外短名（如 sample-channel / pd07843201）
			Joined: tabs[mrlPfx+strconv.FormatInt(id, 10)],
		}
		if g.Name == "" {
			g.Name = "(未知)"
		}
		list = append(list, g)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Joined != list[j].Joined {
			return list[i].Joined
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func guildOpen(a acct) (*sql.DB, []Guild, error) {
	p, err := a.guildPlain()
	if err != nil {
		return nil, nil, err
	}
	db, err := openDB(p)
	if err != nil {
		return nil, nil, err
	}
	gs, err := loadGuilds(db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, gs, nil
}

func cmdGuildList(accts []acct, rest []string) {
	showAll := false
	for _, r := range rest[1:] {
		if r == "--all" {
			showAll = true
		}
	}
	var lastErr error
	for _, a := range accts {
		db, gs, err := guildOpen(a)
		if err != nil {
			lastErr = err
			continue
		}
		db.Close()
		var joined, visited []Guild
		for _, g := range gs {
			if g.Joined {
				joined = append(joined, g)
			} else {
				visited = append(visited, g)
			}
		}
		if optJSON {
			emitJSON(map[string]any{
				"space": a.Space, "qq": a.QQ,
				"joined_count": len(joined), "joined": joined,
				"visited_count": len(visited), "visited": visited,
			})
			return
		}
		for _, g := range joined {
			out(fmt.Sprintf("%s\t%s", g.ID, g.Name))
		}
		if showAll {
			for _, g := range visited {
				out(fmt.Sprintf("%s\t%s\t(未加入)", g.ID, g.Name))
			}
		}
		if !optQuiet {
			fmt.Fprintf(os.Stderr, "# 我加入的频道 %d 个（另有 %d 个仅浏览过）[%s]\n",
				len(joined), len(visited), a.Space)
		}
		return
	}
	die(exitData, fmt.Sprintf("读取频道失败: %v", lastErr))
}

func cmdInGuild(accts []acct, rest []string) {
	key := strings.TrimSpace(guildArg(rest, "in-guild <频道ID|短名|名称>"))
	var lastErr error
	for _, a := range accts {
		db, gs, err := guildOpen(a)
		if err != nil {
			lastErr = err
			continue
		}
		db.Close()
		var hit *Guild
		for i := range gs {
			if gs[i].ID == key || gs[i].Name == key ||
				(gs[i].Short != "" && strings.EqualFold(gs[i].Short, key)) {
				hit = &gs[i]
				break
			}
		}
		if hit == nil {
			if optJSON {
				emitJSON(map[string]any{"key": key, "in_guild": false, "reason": "本机频道库中无此频道"})
			} else {
				fmt.Println("false")
			}
			os.Exit(exitFalse)
		}
		if optJSON {
			emitJSON(map[string]any{"key": key, "id": hit.ID, "name": hit.Name,
				"short": hit.Short, "in_guild": hit.Joined, "space": a.Space})
		} else {
			if hit.Joined {
				fmt.Println("true")
			} else {
				fmt.Println("false")
			}
			if !optQuiet {
				extra := "已加入"
				if !hit.Joined {
					extra = "仅浏览过，未加入"
				}
				fmt.Fprintf(os.Stderr, "# %s [%s] — %s\n", hit.Name, hit.Short, extra)
			}
		}
		if hit.Joined {
			os.Exit(exitOK)
		}
		os.Exit(exitFalse)
	}
	die(exitData, fmt.Sprintf("读取频道失败: %v", lastErr))
}

// guildBodyText 从消息体取正文
//
// 实测结构：外层 field 40800 → 内层 field 45101 才是文本。
func guildBodyText(b []byte) string {
	f := pbLenFields(b)
	if inner, ok := f[40800]; ok {
		for _, ib := range inner {
			ii := pbLenFields(ib)
			if t := pbText(ii, 45101); t != "" {
				return t
			}
		}
	}
	return deepestText(b, 0)
}

// deepestText 兜底：递归找最长的可打印字符串
func deepestText(b []byte, depth int) string {
	if depth > 4 {
		return ""
	}
	best := ""
	f := pbLenFields(b)
	for _, vals := range f {
		for _, v := range vals {
			if s := strings.TrimSpace(string(v)); s != "" && printable(s) {
				if utf8.RuneCountInString(s) > utf8.RuneCountInString(best) {
					best = s
				}
			} else if sub := deepestText(v, depth+1); utf8.RuneCountInString(sub) > utf8.RuneCountInString(best) {
				best = sub
			}
		}
	}
	return best
}

// guildChannelIDs 取某频道下的子频道 ID（t_GPro_Channel_v2.channel_id）
func guildChannelIDs(gdb *sql.DB, guildID string) []int64 {
	rows, err := gdb.Query(`select "channel_id" from t_GPro_Channel_v2 where "guild_id"=?`, guildID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func cmdGuildMsgs(accts []acct, rest []string) {
	g := guildArg(rest, "guild-msgs <频道ID> [--kw 词] [--n N]")
	start := 2
	if len(rest) < 2 || strings.HasPrefix(rest[1], "-") {
		start = 1
	}
	kw, limit := "", 20
	for i := start; i < len(rest); i++ {
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
		}
	}
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	if !optQuiet {
		fmt.Fprintln(os.Stderr, "# 注意：guild-msgs 为实验性功能，结果可能取不全（guild_msg.db 的 WAL 帧加密未解）")
	}
	a := pick(accts)
	// 定位频道：支持 ID / 短名 / 名称（与 in-guild / guild-info 保持一致）
	hit, err := a.resolveGuildKey(g)
	if err != nil {
		die(exitData, err.Error())
	}
	if !hit.Joined {
		die(exitData, "该频道未加入，检索已限定在本人已加入频道范围")
	}
	g = hit.ID
	db, _, err := guildOpen(a)
	if err != nil {
		die(exitData, err.Error())
	}
	// 该频道下的子频道 ID（消息表里用 40027 记录子频道）
	chs := guildChannelIDs(db, g)
	db.Close()
	if len(chs) == 0 {
		die(exitData, "该频道下未找到子频道记录（可能未同步）")
	}
	p, err := a.plainDB("guild_msg.db")
	if err != nil {
		die(exitData, "频道消息库不可用: "+err.Error())
	}
	mdb, err := openDB(p)
	if err != nil {
		die(exitData, err.Error())
	}
	defer mdb.Close()
	ph := make([]string, 0, len(chs))
	args := make([]any, 0, len(chs)+1)
	for _, c := range chs {
		ph = append(ph, "?")
		args = append(args, c)
	}
	args = append(args, limit)
	q := `select "40050","40090","40093","40800" from guild_msg_table where "40027" in (` +
		strings.Join(ph, ",") + `) order by "40050" desc limit ?`
	rows, err := mdb.Query(q, args...)
	if err != nil {
		die(exitData, "查询频道消息失败（库可能未完成同步）: "+err.Error())
	}
	defer rows.Close()
	type hitT struct {
		Ts   int64  `json:"ts"`
		Nick string `json:"nick"`
		Text string `json:"text"`
	}
	var hits []hitT
	for rows.Next() {
		var ts int64
		var nickA, nickB []byte
		var body []byte
		if rows.Scan(&ts, &nickA, &nickB, &body) != nil {
			continue
		}
		nick := strings.TrimSpace(string(nickA))
		if nick == "" {
			nick = strings.TrimSpace(string(nickB))
		}
		txt := guildBodyText(body)
		if kw != "" && !strings.Contains(txt, kw) && !strings.Contains(nick, kw) {
			continue
		}
		if txt == "" {
			continue
		}
		hits = append(hits, hitT{Ts: ts, Nick: nick, Text: txt})
	}
	if len(hits) == 0 {
		die(exitData, "无匹配消息")
	}
	if optJSON {
		emitJSON(hits)
		return
	}
	for _, h := range hits {
		out(fmt.Sprintf("%s\t%s\t%s", timeStr(h.Ts), h.Nick, h.Text))
	}
}
