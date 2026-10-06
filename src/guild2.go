// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// QQ 频道附带数据：子频道、身份组、以及本人在频道内的身份。
//
// 涉及的库表（gpro）：
//
//	t_GPro_Guild_v15                    频道列表，guild_info 为 protobuf
//	                                    （字段 8 = 名称，字段 27 = 对外短名）
//	t_GPro_Channel_v2                   子频道
//	t_GPro_CategoryInfo_v1              子频道分组，msg_channel_info_list 内含该组子频道 id
//	t_GPro_Role                         身份组
//	t_GPro_MemberRoleListInfo_v1_<gid>  成员与身份组的对应关系
//	t_GPro_Preference                   self_tiny_id_db_key 即本人 tiny_id
//
// 已知缺口：t_GPro_MemberInfo_V2_*（频道成员列表）在本机全为 0 行，QQ 未同步该数据，
// 因此只能查询到身份组这一层，查不到具体成员名单。

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// channelTypeLabel 子频道类型（据本机数据归纳，未经官方文档确认）
var channelTypeLabel = map[int64]string{
	1: "文字", 2: "语音", 3: "帖子", 4: "直播", 5: "直播",
	6: "应用", 7: "帖子", 9: "语音房", 10: "签到", 11: "日程", 13: "AI",
}

func ctypeLabel(t int64) string {
	if s, ok := channelTypeLabel[t]; ok {
		return s
	}
	return "type" + strconv.FormatInt(t, 10)
}

func argbHex(v int64) string {
	if v == 0 {
		return ""
	}
	return fmt.Sprintf("#%06X", uint32(v)&0xFFFFFF)
}

func (a acct) myTinyID(db *sql.DB) string {
	var s string
	if db.QueryRow(`select str_value_ from t_GPro_Preference where key_='self_tiny_id_db_key'`).Scan(&s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

// varintsIn 取出 blob 里所有 varint（用于解析 msg_channel_info_list 里的子频道 id）
func varintsIn(b []byte) []uint64 {
	var out []uint64
	i := 0
	for i < len(b) {
		var v uint64
		var s uint
		for i < len(b) && b[i]&0x80 != 0 {
			v |= uint64(b[i]&0x7f) << s
			s += 7
			i++
		}
		if i < len(b) {
			v |= uint64(b[i]&0x7f) << s
			i++
			out = append(out, v)
		}
	}
	return out
}

type chRow struct {
	ID   int64
	Name string
	Type int64
}

func guildChannels(db *sql.DB, g string) []chRow {
	rows, err := db.Query(`select "channel_id","channel_name","channel_type" from t_GPro_Channel_v2
		where "guild_id"=? order by "channel_num","channel_id"`, g)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []chRow
	for rows.Next() {
		var r chRow
		var nm sql.NullString
		if rows.Scan(&r.ID, &nm, &r.Type) == nil {
			r.Name = nm.String
			out = append(out, r)
		}
	}
	return out
}

type roleRow struct {
	ID    int64
	Name  string
	Tag   string
	Desc  string // 等级身份组用 description_ 存 "LV.N"
	Color string
	Count int64
}

// roleDisplay 等级身份组（type_=100）name_ 为空，名称在 description_（如 "LV.5"）
func roleDisplay(r roleRow) string {
	if r.Name != "" {
		return r.Name
	}
	if r.Desc != "" {
		return r.Desc
	}
	return "(无名称)"
}

func guildRoles(db *sql.DB, g string) []roleRow {
	rows, err := db.Query(`select "role_id_","name_","display_tag_name_","description_","color_","count_"
		from t_GPro_Role where "guild_id_"=? order by "role_id_"`, g)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []roleRow
	for rows.Next() {
		var r roleRow
		var n, tag, desc sql.NullString
		var col, cnt sql.NullInt64
		if rows.Scan(&r.ID, &n, &tag, &desc, &col, &cnt) == nil {
			r.Name, r.Tag, r.Desc = n.String, tag.String, desc.String
			r.Color = argbHex(col.Int64)
			r.Count = cnt.Int64
			out = append(out, r)
		}
	}
	return out
}

// guildDetail 频道详情
type guildDetail struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Short       string   `json:"short,omitempty"`
	Joined      bool     `json:"joined"`
	Channels    int      `json:"channels"`
	Roles       int      `json:"roles"`
	MyRoles     []string `json:"my_roles,omitempty"`
	MyRoleIDs   []int64  `json:"my_role_ids,omitempty"`
	FullMemberN int64    `json:"role_total,omitempty"`
}

func loadGuildDetail(db *sql.DB, g string) (*guildDetail, error) {
	d := &guildDetail{ID: g}
	// 频道行
	var info []byte
	err := db.QueryRow(`select guild_info from t_GPro_Guild_v15 where guild_id=?`, g).Scan(&info)
	if err != nil {
		// 不在 Guild_v15 里 → 未加入
		return d, nil
	}
	f := pbLenFields(info)
	d.Name = pbText(f, 8)
	d.Short = pbText(f, 27)
	if d.Name == "" {
		d.Name = "(未知)"
	}
	// MRL 表是否存在 = 已加入
	var tn string
	if db.QueryRow(`select name from sqlite_master where type='table' and name=?`,
		"t_GPro_MemberRoleListInfo_v1_"+g).Scan(&tn) == nil {
		d.Joined = true
	}
	// 计数
	db.QueryRow(`select count(*) from t_GPro_Channel_v2 where "guild_id"=?`, g).Scan(&d.Channels)
	db.QueryRow(`select count(*) from t_GPro_Role where "guild_id_"=?`, g).Scan(&d.Roles)
	// 我的身份组
	if d.Joined {
		roles := guildRoles(db, g)
		byID := map[int64]roleRow{}
		for _, r := range roles {
			byID[r.ID] = r
		}
		if tid := a1myTinyID(db); tid != "" {
			rs, err := db.Query(`select "role_id_" from "`+tn+`" where "tiny_id_"=?`, tid)
			if err == nil {
				for rs.Next() {
					var rid int64
					if rs.Scan(&rid) == nil {
						d.MyRoleIDs = append(d.MyRoleIDs, rid)
						if rr, ok := byID[rid]; ok {
							nm := roleDisplay(rr)
							if rr.Tag != "" && rr.Tag != nm {
								nm += "/" + rr.Tag
							}
							d.MyRoles = append(d.MyRoles, nm)
						} else {
							d.MyRoles = append(d.MyRoles, "role"+strconv.FormatInt(rid, 10))
						}
					}
				}
				rs.Close()
			}
		}
		sort.Strings(d.MyRoles)
	}
	// 该频道"全员"规模（取人数最多的身份组的 count_）
	var maxCnt sql.NullInt64
	db.QueryRow(`select max("count_") from t_GPro_Role where "guild_id_"=?`, g).Scan(&maxCnt)
	d.FullMemberN = maxCnt.Int64
	return d, nil
}

var curTinyID string

func a1myTinyID(db *sql.DB) string {
	if curTinyID != "" {
		return curTinyID
	}
	var s string
	if db.QueryRow(`select str_value_ from t_GPro_Preference where key_='self_tiny_id_db_key'`).Scan(&s) == nil {
		curTinyID = strings.TrimSpace(s)
	}
	return curTinyID
}

func (a acct) guildDetail(g string) (*guildDetail, error) {
	db, _, err := guildOpen(a)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return loadGuildDetail(db, g)
}

// resolveGuildKey 允许用 ID / 短名 / 名称定位
func (a acct) resolveGuildKey(key string) (*Guild, error) {
	db, gs, err := guildOpen(a)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	for i := range gs {
		if gs[i].ID == key || gs[i].Name == key ||
			(gs[i].Short != "" && strings.EqualFold(gs[i].Short, key)) {
			return &gs[i], nil
		}
	}
	return nil, fmt.Errorf("未找到频道: %s（可用 id / 短名 / 名称）", key)
}

func cmdGuildInfo(accts []acct, rest []string) {
	key := guildArg(rest, "guild-info <频道ID|短名|名称>")
	var lastErr error
	for _, a := range accts {
		hit, err := a.resolveGuildKey(key)
		if err != nil {
			lastErr = err
			continue
		}
		d, err := a.guildDetail(hit.ID)
		if err != nil {
			lastErr = err
			continue
		}
		if optJSON {
			m := map[string]any{
				"id": d.ID, "name": d.Name, "short": d.Short, "joined": d.Joined,
				"channels": d.Channels, "roles": d.Roles, "role_total": d.FullMemberN,
				"my_roles": d.MyRoles,
			}
			emitJSON(m)
			return
		}
		out("频道ID : " + d.ID)
		out("名称   : " + d.Name)
		if d.Short != "" {
			out("短名   : " + d.Short)
		}
		out(fmt.Sprintf("已加入 : %v", d.Joined))
		out(fmt.Sprintf("子频道 : %d 个", d.Channels))
		out(fmt.Sprintf("身份组 : %d 个", d.Roles))
		if d.FullMemberN > 0 {
			out(fmt.Sprintf("成员数 : ≥%d（该频道全员身份组人数）", d.FullMemberN))
		}
		if len(d.MyRoles) > 0 {
			out("我的身份组: " + strings.Join(d.MyRoles, " / "))
		}
		return
	}
	die(exitData, fmt.Sprintf("读取失败: %v", lastErr))
}

func cmdGuildChannels(accts []acct, rest []string) {
	key := guildArg(rest, "guild-channels <频道ID|短名|名称>")
	var lastErr error
	for _, a := range accts {
		hit, err := a.resolveGuildKey(key)
		if err != nil {
			lastErr = err
			continue
		}
		db, _, err := guildOpen(a)
		if err != nil {
			die(exitData, err.Error())
		}
		chs := guildChannels(db, hit.ID)
		byID := map[int64]chRow{}
		for _, c := range chs {
			byID[c.ID] = c
		}
		// 分组
		type catT struct {
			name string
			idx  int64
			ids  []int64
		}
		var cats []catT
		rows, err := db.Query(`select "category_name","category_index","msg_channel_info_list"
			from t_GPro_CategoryInfo_v1 where "guild_id"=? order by "category_index"`, hit.ID)
		if err == nil {
			used := map[int64]bool{}
			for rows.Next() {
				var nm sql.NullString
				var idx sql.NullInt64
				var blob []byte
				if rows.Scan(&nm, &idx, &blob) != nil {
					continue
				}
				c := catT{name: nm.String, idx: idx.Int64}
				for _, v := range varintsIn(blob) {
					id := int64(v)
					if _, ok := byID[id]; ok && !used[id] {
						c.ids = append(c.ids, id)
						used[id] = true
					}
				}
				cats = append(cats, c)
			}
			rows.Close()
		}
		db.Close()
		if optJSON {
			type jc struct {
				Category string `json:"category"`
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Type     string `json:"type"`
			}
			var all []jc
			for _, c := range cats {
				for _, id := range c.ids {
					all = append(all, jc{c.name, id, byID[id].Name, ctypeLabel(byID[id].Type)})
				}
			}
			emitJSON(map[string]any{"guild": hit.ID, "name": hit.Name, "channels": all})
			return
		}
		shown := map[int64]bool{}
		var ungrouped []int64
		for _, c := range cats {
			if len(c.ids) == 0 {
				continue
			}
			if c.name == "" { // 无名分组 → 并入"未分组"
				for _, id := range c.ids {
					ungrouped = append(ungrouped, id)
					shown[id] = true
				}
				continue
			}
			out("[" + c.name + "]")
			for _, id := range c.ids {
				shown[id] = true
				nm := byID[id].Name
				if nm == "" {
					nm = "(无名)"
				}
				// 不加行首缩进：保持"制表符分隔 + 行首即字段"，脚本可直接 awk -F'\t'
				out(fmt.Sprintf("%d\t%s\t%s", id, nm, ctypeLabel(byID[id].Type)))
			}
		}
		var rest2 []chRow
		for _, c := range chs {
			if !shown[c.ID] {
				rest2 = append(rest2, c)
			}
		}
		_ = ungrouped
		if len(rest2) > 0 {
			out("[未分组]")
			for _, c := range rest2 {
				nm := c.Name
				if nm == "" {
					nm = "(无名)"
				}
				out(fmt.Sprintf("%d\t%s\t%s", c.ID, nm, ctypeLabel(c.Type)))
			}
		}
		if !optQuiet {
			fmt.Fprintf(os.Stderr, "# %s 共 %d 个子频道（%d 个分组）\n", hit.Name, len(chs), len(cats))
		}
		return
	}
	die(exitData, fmt.Sprintf("读取失败: %v", lastErr))
}

func cmdGuildRoles(accts []acct, rest []string) {
	key := guildArg(rest, "guild-roles <频道ID|短名|名称>")
	var lastErr error
	for _, a := range accts {
		hit, err := a.resolveGuildKey(key)
		if err != nil {
			lastErr = err
			continue
		}
		db, _, err := guildOpen(a)
		if err != nil {
			die(exitData, err.Error())
		}
		rs := guildRoles(db, hit.ID)
		db.Close()
		if optJSON {
			emitJSON(map[string]any{"guild": hit.ID, "name": hit.Name, "roles": rs})
			return
		}
		for _, r := range rs {
			nm := roleDisplay(r)
			line := fmt.Sprintf("%d\t%s", r.ID, nm)
			if r.Tag != "" {
				line += "\t" + r.Tag
			}
			if r.Color != "" {
				line += "\t" + r.Color
			}
			line += fmt.Sprintf("\t%d人", r.Count)
			out(line)
		}
		if !optQuiet {
			fmt.Fprintf(os.Stderr, "# %s 共 %d 个身份组\n", hit.Name, len(rs))
		}
		return
	}
	die(exitData, fmt.Sprintf("读取失败: %v", lastErr))
}
