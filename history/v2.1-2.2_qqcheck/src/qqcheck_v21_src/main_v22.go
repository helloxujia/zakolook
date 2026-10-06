// qqcheck — QQ 本地数据只读查询工具（Go 编译版）
// =====================================================
// 仅限本人 / 已授权设备与账号使用。
// 全程本地只读：不联网、不修改 QQ 数据、不上传任何内容。
//
// 命令:
//   qqcheck check  <号码>                综合体检（自动判定 群/用户）
//   qqcheck group  <群号>                群报告
//   qqcheck user   <QQ号>                用户报告
//   qqcheck member <群号> <QQ号|uid>     成员角色（群主/管理员/头衔）
//   qqcheck uid    <QQ号>                号码 -> 内部UID 映射
//   qqcheck search <关键词>              关键位置全文搜索（含上下文）
//   qqcheck overview                     账号总览
//
// 选项:
//   --json               JSON 输出（check/group/user/member/uid/overview）
//   --space main|dual    限定空间
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "2.0"

var rootMain = "/data/data/com.tencent.mobileqq"
var tmpDir = "/data/local/tmp/qqcheck/tmp"
var hasSQL = false

type Space struct {
	Name string
	Path string
}

var spaces []Space

var fileCache = map[string][]byte{}

func rdc(path string) []byte {
	if b, ok := fileCache[path]; ok {
		return b
	}
	b, err := os.ReadFile(path)
	if err != nil {
		b = nil
	}
	fileCache[path] = b
	return b
}

func mtimeStr(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return st.ModTime().Format("2006-01-02 15:04")
}

func tsStr(v int64) string {
	if v <= 0 {
		return "0"
	}
	if v > 1_000_000_000_000 {
		v /= 1000
	}
	if v < 1_000_000_000 || v > 5_000_000_000 {
		return strconv.FormatInt(v, 10)
	}
	return time.Unix(v, 0).Format("2006-01-02 15:04:05")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func globMaybe(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

func contexts(data []byte, tok string, before, after, limit int) []string {
	var out []string
	tb := []byte(tok)
	s := 0
	for len(out) < limit {
		i := bytes.Index(data[s:], tb)
		if i < 0 {
			break
		}
		i += s
		lo := i - before
		if lo < 0 {
			lo = 0
		}
		hi := i + len(tb) + after
		if hi > len(data) {
			hi = len(data)
		}
		out = append(out, strings.ReplaceAll(string(data[lo:hi]), "\n", " "))
		s = i + 1
	}
	return out
}

// ---------------- 空间 ----------------
func detectSpaces() []Space {
	var out []Space
	if v := os.Getenv("QQCHECK_ROOT"); v != "" {
		return []Space{{"主空间", v}}
	}
	if isDir(rootMain) {
		out = append(out, Space{"主空间", rootMain})
	}
	re := regexp.MustCompile(`/data/user/(\d+)/`)
	for _, p := range globMaybe("/data/user/*/com.tencent.mobileqq") {
		m := re.FindStringSubmatch(p)
		if m != nil && m[1] != "0" && isDir(p) {
			out = append(out, Space{"分身(u" + m[1] + ")", p})
		}
	}
	return out
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func checkRoot() {
	ok := false
	for _, sp := range spaces {
		if _, err := os.ReadDir(sp.Path); err == nil {
			ok = true
		}
	}
	if !ok {
		fmt.Println("[错误] 无法读取 QQ 数据目录（需要 root）。请用: su -c \"qqcheck ...\"")
		os.Exit(3)
	}
}

// ---------------- 数据读取 ----------------
var reUidKey = regexp.MustCompile(`uid_prefix_key_(\d{5,12})`)
var reU = regexp.MustCompile(`u_[A-Za-z0-9_\-]{10,44}`)
var reTitle = regexp.MustCompile(`"(\d{5,12})-(u_[A-Za-z0-9_\-]{10,44})":\{[^}]*\}`)
var reC = regexp.MustCompile(`"c":"([^"]*)"`)

type tEnt struct{ Group, UID, Title string }

func uidMap(sp Space) map[string]string {
	data := rdc(sp.Path + "/files/mmkv/qq_uin_uid_map")
	mp := map[string]string{}
	for _, loc := range reUidKey.FindAllSubmatchIndex(data, -1) {
		uin := string(data[loc[2]:loc[3]])
		if _, ok := mp[uin]; ok {
			continue
		}
		start := loc[1]
		end := start + 95
		if end > len(data) {
			end = len(data)
		}
		win := data[start:end]
		if i := bytes.Index(win, []byte("uid_prefix_key_")); i >= 0 {
			win = win[:i]
		}
		if m := reU.Find(win); m != nil {
			mp[uin] = string(m)
		}
	}
	return mp
}

func revAll() map[string]string {
	mp := map[string]string{}
	for _, sp := range spaces {
		for k, v := range uidMap(sp) {
			if _, ok := mp[v]; !ok {
				mp[v] = k
			}
		}
	}
	return mp
}

func titleEntries(sp Space) []tEnt {
	data := rdc(sp.Path + "/files/mmkv/common_mmkv_configurations")
	var out []tEnt
	for _, m := range reTitle.FindAllSubmatch(data, -1) {
		t := ""
		if cm := reC.FindSubmatch(m[0]); cm != nil {
			t = strings.TrimSpace(string(cm[1]))
		}
		out = append(out, tEnt{string(m[1]), string(m[2]), t})
	}
	return out
}

func accountNumbers(sp Space) []string {
	set := map[string]bool{}
	for _, f := range globMaybe(sp.Path + "/databases/*.db") {
		b := strings.TrimSuffix(filepath.Base(f), ".db")
		if isDigits(b) {
			set[b] = true
		}
	}
	for _, f := range globMaybe(sp.Path + "/shared_prefs/*.xml") {
		b := strings.TrimSuffix(filepath.Base(f), ".xml")
		if isDigits(b) {
			set[b] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func numsAll() map[string]bool {
	s := map[string]bool{}
	for _, sp := range spaces {
		for _, n := range accountNumbers(sp) {
			s[n] = true
		}
	}
	return s
}

// ---------------- 扫描 ----------------
type Hit struct {
	Name  string
	Count int
	Mtime string
}

func scanNumber(sp Space, tok string) (prefs, mmkv, db []Hit) {
	tb := []byte(tok)
	for _, f := range globMaybe(sp.Path + "/shared_prefs/*") {
		if !isFile(f) {
			continue
		}
		d := rdc(f)
		if n := bytes.Count(d, tb); n > 0 {
			prefs = append(prefs, Hit{filepath.Base(f), n, mtimeStr(f)})
		}
	}
	for _, f := range globMaybe(sp.Path + "/files/mmkv/*") {
		if !isFile(f) || strings.HasSuffix(f, ".crc") {
			continue
		}
		if fileSize(f) > 96*1024*1024 {
			continue
		}
		d := rdc(f)
		if n := bytes.Count(d, tb); n > 0 {
			mmkv = append(mmkv, Hit{filepath.Base(f), n, mtimeStr(f)})
		}
	}
	for _, pat := range []string{sp.Path + "/databases/*.db", sp.Path + "/databases/*.db-wal"} {
		for _, f := range globMaybe(pat) {
			if !isFile(f) {
				continue
			}
			d := rdc(f)
			if n := bytes.Count(d, tb); n > 0 {
				db = append(db, Hit{filepath.Base(f), n, mtimeStr(f)})
			}
		}
	}
	return
}

// ---------------- 旧版明文库 ----------------
func openLegacyCopy(sp Space) (string, bool) {
	var cands []string
	for _, f := range globMaybe(sp.Path + "/databases/*.db") {
		b := strings.TrimSuffix(filepath.Base(f), ".db")
		if isDigits(b) {
			cands = append(cands, f)
		}
	}
	if len(cands) == 0 {
		return "", false
	}
	sort.Slice(cands, func(i, j int) bool { return fileSize(cands[i]) > fileSize(cands[j]) })
	src := cands[0]
	_ = os.MkdirAll(tmpDir, 0755)
	cp := filepath.Join(tmpDir, filepath.Base(src))
	need := true
	if st1, e1 := os.Stat(cp); e1 == nil {
		if st2, e2 := os.Stat(src); e2 == nil && st1.Size() == st2.Size() && !st1.ModTime().Before(st2.ModTime()) {
			need = false
		}
	}
	if need {
		for _, suf := range []string{"", "-wal", "-shm"} {
			if b, err := os.ReadFile(src + suf); err == nil {
				_ = os.WriteFile(cp+suf, b, 0644)
			}
		}
	}
	return cp, true
}

type GEvid struct {
	FileTable                bool
	FileTableN               int
	FileTableMin, FileTableMax int64
	FileMgrN                 int
	FileMgrMin, FileMgrMax   int64
	NotifN                   int
	NotifMax                 int64
	EssN                     int
	EssMax                   int64
}

// ---------------- 判定类型 ----------------
func detectKind(tok string) (gs, us, chs []string) {
	// v2.2: 增加频道(channel)识别。
	//   判据：号码出现在频道缓存结构中（str_guild_id / guild_id）。
	//   注意：本地频道数据仅为 feed 浏览缓存，证据强度弱于群，输出时须如实标注。
	for _, sp := range spaces {
		for _, f := range globMaybe(sp.Path + "/files/mmkv/guild_mmkv_configurations*") {
			dd := rdc(f)
			if bytes.Contains(dd, []byte(`"str_guild_id":"`+tok+`"`)) ||
				bytes.Contains(dd, []byte(`"guild_id":`+tok)) ||
				bytes.Contains(dd, []byte(`"guild_id":"`+tok+`"`)) {
				chs = append(chs, sp.Name+"：频道缓存中出现该编号")
				break
			}
		}
	}
	tb := regexp.MustCompile(`"` + regexp.QuoteMeta(tok) + `-u_[A-Za-z0-9_\-]{6,44}"`)
	for _, sp := range spaces {
		d := rdc(sp.Path + "/files/mmkv/common_mmkv_configurations")
		if tb.Match(d) {
			gs = append(gs, sp.Name+"：成员头衔缓存含 \"<群号>-uid\" 键")
		}
		for _, f := range globMaybe(sp.Path + "/databases/*.db") {
			if bytes.Contains(rdc(f), []byte("TroopFileTansferItemEntity"+tok)) {
				gs = append(gs, sp.Name+"：存在该群的群文件记录表")
				break
			}
		}
		um := uidMap(sp)
		if uid, ok := um[tok]; ok {
			us = append(us, sp.Name+"：号码→UID 映射（"+uid+"）")
			for _, f := range globMaybe(sp.Path + "/files/mmkv/q_chat_item_mmkv_file_*") {
				if bytes.Contains(rdc(f), []byte(uid)) {
					us = append(us, sp.Name+"：聊天列表出现（uid 形式）")
					break
				}
			}
		}
		if bytes.Contains(d, []byte("QQMutualMark_"+tok+"_")) {
			us = append(us, sp.Name+"：好友互动标识痕迹")
		}
	}
	return
}

// ---------------- 报告：群 ----------------
type Evidence struct {
	Text  string `json:"text"`
	Score int    `json:"score"`
}
type AccountRep struct {
	Account  string     `json:"account"`
	Evidence []Evidence `json:"evidence"`
	Score    int        `json:"score"`
	Verdict  string     `json:"verdict"`
}
type RoleCount struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}
type SpaceGroupRep struct {
	Space       string            `json:"space"`
	Accounts    []AccountRep      `json:"accounts"`
	MemberCount int               `json:"member_count"`
	Roles       []RoleCount       `json:"roles,omitempty"`
	Owner       []string          `json:"owner,omitempty"`
	Admins      []string          `json:"admins,omitempty"`
	SelfTitles  map[string]string `json:"self_titles,omitempty"`
}
type GroupResult struct {
	Group       string          `json:"group"`
	Generated   string          `json:"generated"`
	Spaces      []SpaceGroupRep `json:"spaces"`
	MemberTotal int             `json:"member_total"`
	// [v2.1] 机器友好字段：调用方无需解析 verdict 自然语言措辞
	InGroup    bool   `json:"in_group"`   // 是否在群（best_score > 0）
	BestScore  int    `json:"best_score"` // 所有空间账号中的最高证据分
	Confidence string `json:"confidence"` // high(>=6)/medium(>=3)/weak(>0)/none(0)
}

// ---------------- 报告：频道 ----------------
// v2.2 新增。本地频道数据来源仅 guild_mmkv_configurations*（feed 浏览缓存），
// 无法证明"已加入"，故不设 in_group 语义，仅报告痕迹是否存在。
type ChannelResult struct {
	Channel   string   `json:"channel"`
	Generated string   `json:"generated"`
	Kind      string   `json:"kind"`
	IDLength  int      `json:"id_length"`
	InChannel bool     `json:"in_channel"`
	Evidence  []string `json:"evidence,omitempty"`
	Note      string   `json:"note"`
}

func reportChannel(tok string) ChannelResult {
	res := ChannelResult{
		Channel:   tok,
		Generated: time.Now().Format("2006-01-02 15:04:05"),
		Kind:      "channel",
		IDLength:  len(tok),
		Note:      "本地频道数据仅含浏览缓存，不能证明已加入该频道",
	}
	_, _, chs := detectKind(tok)
	res.Evidence = chs
	res.InChannel = len(chs) > 0
	return res
}

func renderChannel(res ChannelResult) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  频道报告：" + res.Channel)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()
	fmt.Printf("  类型：频道（guild_id）  编号长度：%d 位\n", res.IDLength)
	fmt.Printf("  生成时间：%s\n", res.Generated)
	fmt.Println()
	if res.InChannel {
		fmt.Println("  本地痕迹：存在")
		for _, e := range res.Evidence {
			fmt.Println("    · " + e)
		}
	} else {
		fmt.Println("  本地痕迹：未发现")
	}
	fmt.Println()
	fmt.Println("  说明：" + res.Note)
	fmt.Println()
}

func reportGroup(g string) GroupResult {
	res := GroupResult{Group: g, Generated: time.Now().Format("2006-01-02 15:04:05")}
	rev := revAll()
	merged := map[string]string{}
	for _, sp := range spaces {
		for _, e := range titleEntries(sp) {
			if e.Group == g {
				merged[e.UID] = e.Title
			}
		}
	}
	res.MemberTotal = len(merged)
	na := numsAll()
	for _, sp := range spaces {
		r := SpaceGroupRep{Space: sp.Name}
		cp, okcp := openLegacyCopy(sp)
		var ev GEvid
		if okcp {
			ev = dbGroupEvidence(cp, g)
		}
		rawTable := false
		if okcp {
			rawTable = bytes.Contains(rdc(cp), []byte("TroopFileTansferItemEntity"+g))
		}
		for _, acc := range accountNumbers(sp) {
			if len(acc) < 6 {
				continue
			}
			a := AccountRep{Account: acc}
			add := func(t string, s int) { a.Evidence = append(a.Evidence, Evidence{t, s}) }
			if ev.FileTable {
				add(fmt.Sprintf("群文件记录表：%d条（%s → %s）", ev.FileTableN, tsStr(ev.FileTableMin), tsStr(ev.FileTableMax)), 3)
			} else if rawTable {
				add("群文件记录表：存在", 3)
			}
			if ev.FileMgrN > 0 {
				add(fmt.Sprintf("群文件管理：%d条（%s → %s）", ev.FileMgrN, tsStr(ev.FileMgrMin), tsStr(ev.FileMgrMax)), 2)
			}
			if ev.NotifN > 0 {
				add(fmt.Sprintf("群通知缓存：%d条（最新 %s）", ev.NotifN, tsStr(ev.NotifMax)), 1)
			}
			if ev.EssN > 0 {
				add(fmt.Sprintf("群精华消息：%d条（最新 %s）", ev.EssN, tsStr(ev.EssMax)), 1)
			}
			pf := sp.Path + "/shared_prefs/" + acc + "_" + g + ".xml"
			if isFile(pf) {
				add("账号×群专属配置：存在（改于 "+mtimeStr(pf)+"）", 3)
			}
			cnt, last := 0, ""
			for _, f := range globMaybe(sp.Path + "/shared_prefs/troop_gt_af_daily_" + acc + "_*.xml") {
				if bytes.Contains(rdc(f), []byte(g)) {
					cnt++
					if b := filepath.Base(f); b > last {
						last = b
					}
				}
			}
			if cnt > 0 {
				d := last
				if len(d) > 14 {
					d = d[len(d)-14 : len(d)-4]
				}
				add(fmt.Sprintf("群每日记录：%d 天提及（最新 %s）", cnt, d), 2)
			}
			lu := rdc(sp.Path + "/shared_prefs/last_update_time" + acc + ".xml")
			reLU := regexp.MustCompile(`key_last_update_time` + g + `" value="(\d+)"`)
			if m := reLU.FindSubmatch(lu); m != nil {
				if v, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil {
					add("群最后更新："+tsStr(v), 1)
				}
			}
			for _, f := range globMaybe(sp.Path + "/files/mmkv/q_chat_item_mmkv_file_" + acc) {
				if bytes.Contains(rdc(f), []byte(g)) {
					add("聊天列表：在列（文件更新于 "+mtimeStr(f)+"）", 3)
				}
			}
			for _, e := range a.Evidence {
				a.Score += e.Score
			}
			switch {
			case a.Score >= 6:
				a.Verdict = "已加入该群（强证据）"
			case a.Score >= 3:
				a.Verdict = "在该群（中等证据）"
			case a.Score > 0:
				a.Verdict = "仅弱痕迹"
			default:
				a.Verdict = "未发现证据"
			}
			r.Accounts = append(r.Accounts, a)
		}
		mem := map[string]string{}
		for _, e := range titleEntries(sp) {
			if e.Group == g {
				mem[e.UID] = e.Title
			}
		}
		r.MemberCount = len(mem)
		if len(mem) > 0 {
			cnt := map[string]int{}
			for _, t := range mem {
				if t == "" {
					t = "(无头衔)"
				}
				cnt[t]++
			}
			var rc []RoleCount
			for t, n := range cnt {
				rc = append(rc, RoleCount{t, n})
			}
			sort.Slice(rc, func(i, j int) bool { return rc[i].Count > rc[j].Count })
			r.Roles = rc
			for u, t := range mem {
				name := rev[u]
				if name == "" {
					name = "?"
				}
				disp := u + "(" + name + ")"
				if strings.Contains(t, "群主") {
					r.Owner = append(r.Owner, disp)
				}
				if t == "管理员" {
					r.Admins = append(r.Admins, disp)
				}
			}
			sort.Strings(r.Owner)
			sort.Strings(r.Admins)
			st := map[string]string{}
			for u, t := range mem {
				if name, ok := rev[u]; ok && na[name] {
					st[name] = t
				}
			}
			if len(st) > 0 {
				r.SelfTitles = st
			}
		}
		res.Spaces = append(res.Spaces, r)
	}
	// [v2.1] 汇总：所有空间所有账号的最高分 → 派生 in_group/confidence
	best := 0
	for _, sp := range res.Spaces {
		for _, a := range sp.Accounts {
			if a.Score > best {
				best = a.Score
			}
		}
	}
	res.BestScore = best
	res.InGroup = best > 0
	switch {
	case best >= 6:
		res.Confidence = "high"
	case best >= 3:
		res.Confidence = "medium"
	case best > 0:
		res.Confidence = "weak"
	default:
		res.Confidence = "none"
	}
	return res
}

func renderGroup(res GroupResult) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println(" 群报告：" + res.Group)
	fmt.Println(strings.Repeat("=", 60))
	for _, r := range res.Spaces {
		fmt.Println()
		fmt.Println("【" + r.Space + "】")
		for _, a := range r.Accounts {
			fmt.Printf("  账号 %s  →  %s\n", a.Account, a.Verdict)
			for _, e := range a.Evidence {
				fmt.Println("      · " + e.Text)
			}
			if len(a.Evidence) == 0 {
				fmt.Println("      · （无证据）")
			}
		}
		if r.MemberCount > 0 {
			fmt.Printf("  成员头衔缓存：%d 人\n", r.MemberCount)
			var parts []string
			for i, rc := range r.Roles {
				if i >= 8 {
					break
				}
				parts = append(parts, fmt.Sprintf("%s×%d", rc.Title, rc.Count))
			}
			fmt.Println("    角色分布：" + strings.Join(parts, "；"))
			if len(r.Owner) > 0 {
				fmt.Println("    群主：" + strings.Join(r.Owner, "，"))
			}
			if len(r.Admins) > 0 {
				fmt.Println("    管理员：" + strings.Join(r.Admins, "，"))
			}
			if len(r.SelfTitles) > 0 {
				var sp []string
				for k, v := range r.SelfTitles {
					sp = append(sp, k+"="+v)
				}
				sort.Strings(sp)
				fmt.Println("    本机账号头衔：" + strings.Join(sp, "；"))
			}
		}
	}
	fmt.Println()
	if !hasSQL {
		fmt.Println("（提示：当前为 lite 构建，SQL 统计项不可用；完整版请使用 -tags sql 构建）")
	}
	fmt.Println("（本地只读分析；权威状态以 QQ 内实际显示为准）")
}

// ---------------- 报告：用户 ----------------
type GroupRole struct {
	Group string `json:"group"`
	Title string `json:"title"`
}
type SpaceUserRep struct {
	Space   string      `json:"space"`
	UID     string      `json:"uid,omitempty"`
	Groups  []GroupRole `json:"groups,omitempty"`
	Marks   []string    `json:"marks,omitempty"`
	Signals []string    `json:"signals,omitempty"`
	NHits   int         `json:"hits_prefs"`
	MHits   int         `json:"hits_mmkv"`
	DHits   int         `json:"hits_db"`
	MMKVTop []string    `json:"mmkv_top,omitempty"`
}
type UserResult struct {
	User      string         `json:"user"`
	Generated string         `json:"generated"`
	Spaces    []SpaceUserRep `json:"spaces"`
}

func reportUser(uin string) UserResult {
	res := UserResult{User: uin, Generated: time.Now().Format("2006-01-02 15:04:05")}
	var allt []tEnt
	for _, sp := range spaces {
		allt = append(allt, titleEntries(sp)...)
	}
	reMark := regexp.MustCompile("QQMutualMark_" + regexp.QuoteMeta(uin) + "(_?)([^_\x00\"]{2,80})_([0-9]{5,12})")
	reSound := regexp.MustCompile(`<string name="special_sound\d+">([^<]*)</string>`)
	reSpc := regexp.MustCompile(`spcares[^0-9;\x00<]{0,6}([0-9;]{10,400})`)
	tokPat := regexp.MustCompile("(^|[^0-9])" + uin + "([^0-9]|$)")
	for _, sp := range spaces {
		r := SpaceUserRep{Space: sp.Name}
		um := uidMap(sp)
		uid := um[uin]
		r.UID = uid
		key := uid
		if key == "" {
			key = uin
		}
		if uid != "" {
			seen := map[string]bool{}
			for _, e := range allt {
				if e.UID == uid && !seen[e.Group] {
					seen[e.Group] = true
					t := e.Title
					if t == "" {
						t = "(无头衔)"
					}
					r.Groups = append(r.Groups, GroupRole{e.Group, t})
				}
			}
		}
		d := rdc(sp.Path + "/files/mmkv/common_mmkv_configurations")
		seenM := map[string]bool{}
		for _, m := range reMark.FindAllSubmatch(d, -1) {
			mk := string(m[2])
			if !seenM[mk] {
				seenM[mk] = true
				r.Marks = append(r.Marks, mk)
			}
		}
		for _, acc := range accountNumbers(sp) {
			pf := sp.Path + "/shared_prefs/pai_yi_pai_user_double_tap_timestamp_" + acc + ".xml"
			if !isFile(pf) {
				continue
			}
			pd := rdc(pf)
			for _, k := range []string{uin, uid} {
				if k == "" {
					continue
				}
				rePai := regexp.MustCompile(`name="` + regexp.QuoteMeta(k) + `" value="(\d+)"`)
				if m := rePai.FindSubmatch(pd); m != nil {
					if v, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil {
						r.Signals = append(r.Signals, "拍一拍："+tsStr(v))
					}
				}
			}
		}
		d2 := rdc(sp.Path + "/shared_prefs/com.tencent.mobileqq_preferences.xml")
		for _, m := range reSound.FindAllSubmatch(d2, -1) {
			if tokPat.Match(m[1]) {
				r.Signals = append(r.Signals, "特别关心/特殊提示音名单：在列")
				break
			}
		}
		if m := reSpc.FindSubmatch(d); m != nil {
			for _, x := range strings.Split(string(m[1]), ";") {
				if x == uin {
					r.Signals = append(r.Signals, "spcares(特别关心)名单：在列")
					break
				}
			}
		}
		for _, f := range globMaybe(sp.Path + "/files/mmkv/q_chat_item_mmkv_file_*") {
			qd := rdc(f)
			if bytes.Contains(qd, []byte(key)) || bytes.Contains(qd, []byte(uin)) {
				r.Signals = append(r.Signals, "聊天列表：在列（"+filepath.Base(f)+"）")
				break
			}
		}
		pr, mm, dbh := scanNumber(sp, uin)
		r.NHits, r.MHits, r.DHits = len(pr), len(mm), len(dbh)
		for i, h := range mm {
			if i >= 6 {
				break
			}
			r.MMKVTop = append(r.MMKVTop, fmt.Sprintf("%s（%d处, %s）", h.Name, h.Count, h.Mtime))
		}
		res.Spaces = append(res.Spaces, r)
	}
	return res
}

func renderUser(res UserResult) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println(" 用户报告：" + res.User)
	fmt.Println(strings.Repeat("=", 60))
	anyg := false
	for _, r := range res.Spaces {
		fmt.Println()
		if r.UID != "" {
			fmt.Println("【" + r.Space + "】UID: " + r.UID)
		} else {
			fmt.Println("【" + r.Space + "】号码未映射到 UID")
		}
		if len(r.Groups) > 0 {
			anyg = true
			fmt.Println("  群成员角色：")
			for _, g := range r.Groups {
				fmt.Println("    · 群 " + g.Group + " → " + g.Title)
			}
		}
		if len(r.Marks) > 0 {
			fmt.Println("  互动标识：" + strings.Join(r.Marks, "、"))
		}
		for _, s := range r.Signals {
			fmt.Println("  · " + s)
		}
		if r.NHits+r.MHits+r.DHits > 0 {
			fmt.Printf("  文件命中：prefs %d 个 / mmkv %d 个 / 数据库 %d 个\n", r.NHits, r.MHits, r.DHits)
			for _, t := range r.MMKVTop {
				fmt.Println("      · [mmkv] " + t)
			}
		}
	}
	if !anyg {
		fmt.Println()
		fmt.Println("（未发现该用户出现在\"群成员头衔缓存\"中）")
	}
	fmt.Println()
	fmt.Println("（本地只读分析；\"是否好友\"以互动痕迹推断，权威状态以 QQ 内为准）")
}

// ---------------- 报告：成员 ----------------
type MemberResult struct {
	Group    string         `json:"group"`
	Key      string         `json:"key"`
	UID      string         `json:"uid,omitempty"`
	Uin      string         `json:"uin,omitempty"`
	Title    string         `json:"title,omitempty"`
	Found    bool           `json:"found"`
	Entries  int            `json:"entries"`
	Raw      []string       `json:"raw"`
	Controls map[string]any `json:"controls,omitempty"`
	Error    string         `json:"error,omitempty"`
}

func reportMember(g, key string) MemberResult {
	res := MemberResult{Group: g, Key: key}
	rev := revAll()
	uid := ""
	if strings.HasPrefix(key, "u_") {
		uid = key
		res.UID = uid
		res.Uin = rev[uid]
	} else {
		for _, sp := range spaces {
			if v, ok := uidMap(sp)[key]; ok {
				uid = v
				res.UID = uid
				res.Uin = key
				break
			}
		}
	}
	if uid == "" {
		res.Error = "未找到该号码的 UID 映射（可能未出现在缓存中）"
		return res
	}
	for _, sp := range spaces {
		d := rdc(sp.Path + "/files/mmkv/common_mmkv_configurations")
		pat := []byte(`"` + g + "-" + uid + `"`)
		res.Raw = append(res.Raw, fmt.Sprintf("%s: %d处", sp.Name, bytes.Count(d, pat)))
	}
	var titles []string
	for _, sp := range spaces {
		for _, e := range titleEntries(sp) {
			if e.Group == g && e.UID == uid {
				titles = append(titles, e.Title)
			}
		}
	}
	res.Entries = len(titles)
	if len(titles) > 0 {
		t := titles[len(titles)-1]
		if t == "" {
			t = "(空头衔)"
		}
		res.Title = t
		res.Found = true
	}
	mem := map[string]string{}
	for _, sp := range spaces {
		for _, e := range titleEntries(sp) {
			if e.Group == g {
				mem[e.UID] = e.Title
			}
		}
	}
	if len(mem) > 0 {
		roles := map[string]int{}
		var owner, admins []string
		for u, t := range mem {
			if t == "" {
				t = "(无)"
			}
			roles[t]++
			if strings.Contains(t, "群主") {
				owner = append(owner, u)
			}
			if t == "管理员" {
				admins = append(admins, u)
			}
		}
		var rc []RoleCount
		for t, n := range roles {
			rc = append(rc, RoleCount{t, n})
		}
		sort.Slice(rc, func(i, j int) bool { return rc[i].Count > rc[j].Count })
		res.Controls = map[string]any{"members": len(mem), "roles": rc, "owner": owner, "admins": admins}
	}
	return res
}

func renderMember(res MemberResult) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println(" 成员角色查询：群 " + res.Group + " × " + res.Key)
	fmt.Println(strings.Repeat("=", 60))
	if res.Error != "" {
		fmt.Println("[!] " + res.Error)
		return
	}
	line := "  UID: " + res.UID
	if res.Uin != "" {
		line += "（QQ号 " + res.Uin + "）"
	}
	fmt.Println(line)
	if res.Found {
		fmt.Printf("  头衔条目：\"c\":\"%s\"（命中 %d 次）\n", res.Title, res.Entries)
	} else {
		fmt.Println("  未在成员头衔缓存中找到该成员（可能不在群/未缓存）")
	}
	if res.Controls != nil {
		roles, _ := res.Controls["roles"].([]RoleCount)
		var parts []string
		for i, rc := range roles {
			if i >= 6 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s×%d", rc.Title, rc.Count))
		}
		fmt.Printf("  同群对照：共 %v 人缓存；角色分布：%s\n", res.Controls["members"], strings.Join(parts, "；"))
		if o, ok := res.Controls["owner"].([]string); ok && len(o) > 0 {
			fmt.Println("    群主：" + strings.Join(o, "，"))
		}
		if a, ok := res.Controls["admins"].([]string); ok && len(a) > 0 {
			fmt.Printf("    管理员（%d）：%s\n", len(a), strings.Join(a, "，"))
		}
	}
	if res.Found && res.Title != "" {
		fmt.Printf("  判定：该成员在群 %s 的角色/头衔为【%s】\n", res.Group, res.Title)
	}
	fmt.Println()
	fmt.Println("（本地只读分析；权威状态以 QQ 内为准）")
}

// ---------------- 报告：总览 ----------------
type SpaceOverview struct {
	Space     string            `json:"space"`
	Accounts  []string          `json:"accounts"`
	Spcares   []string          `json:"spcares,omitempty"`
	Groups    []string          `json:"groups_with_files,omitempty"`
	Freshness map[string]string `json:"freshness"`
}

func reportOverview() []SpaceOverview {
	var res []SpaceOverview
	reSpc := regexp.MustCompile(`spcares[^0-9;\x00<]{0,6}([0-9;]{10,400})`)
	for _, sp := range spaces {
		r := SpaceOverview{Space: sp.Name, Accounts: accountNumbers(sp), Freshness: map[string]string{}}
		d := rdc(sp.Path + "/files/mmkv/common_mmkv_configurations")
		if m := reSpc.FindSubmatch(d); m != nil {
			for _, x := range strings.Split(string(m[1]), ";") {
				if x != "" {
					r.Spcares = append(r.Spcares, x)
				}
			}
		}
		if cp, ok := openLegacyCopy(sp); ok {
			r.Groups = dbGroupsWithFiles(cp)
		}
		for _, k := range []string{"shared_prefs", "files/mmkv", "databases"} {
			r.Freshness[k] = mtimeStr(sp.Path + "/" + k)
		}
		res = append(res, r)
	}
	return res
}

func renderOverview(res []SpaceOverview) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println(" 账号总览")
	fmt.Println(strings.Repeat("=", 60))
	for _, r := range res {
		fmt.Println()
		fmt.Println("【" + r.Space + "】")
		if len(r.Accounts) > 0 {
			fmt.Println("  账号：" + strings.Join(r.Accounts, "、"))
		} else {
			fmt.Println("  账号：?")
		}
		if len(r.Spcares) > 0 {
			fmt.Println("  spcares(特别关心)名单：" + strings.Join(r.Spcares, "、"))
		}
		if len(r.Groups) > 0 {
			disp := r.Groups
			if len(disp) > 15 {
				disp = disp[:15]
			}
			fmt.Printf("  有文件记录的群（%d）：%s\n", len(r.Groups), strings.Join(disp, "、"))
		}
		for _, k := range []string{"shared_prefs", "files/mmkv", "databases"} {
			fmt.Printf("  %s 目录改动于：%s\n", k, r.Freshness[k])
		}
	}
}

// ---------------- 入口 ----------------
func usage() {
	fmt.Println(`qqcheck — QQ 本地数据只读查询工具（Go版 v` + version + `）
仅限本人 / 已授权设备与账号使用；全程本地只读：不联网、不修改。

命令:
  qqcheck check  <号码>                综合体检（自动判定 群/用户）
  qqcheck group  <群号>                群报告（是否在群 + 证据 + 成员统计）
  qqcheck user   <QQ号>                用户报告（映射 + 互动痕迹 + 所在群角色）
  qqcheck member <群号> <QQ号|uid>     成员角色（群主/管理员/头衔）
  qqcheck uid    <QQ号>                号码 -> 内部UID 映射
  qqcheck search <关键词>              关键位置全文搜索（含上下文）
  qqcheck overview                     账号总览

选项:
  --json               JSON 输出（check/group/user/member/uid/overview）
  --space main|dual    限定空间`)
}

func die(msg string) {
	fmt.Println("[错误] " + msg)
	os.Exit(2)
}

func emitJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// emitGroupOutcome 统一处理群报告的输出与退出码：
//   --in-group   -> 仅输出 true/false
//   --json       -> 结构化 JSON（含 in_group/best_score/confidence）
//   默认         -> 人类可读报告
//   --strict-exit-> 以退出码表达结果：0=在群，1=不在群
func emitGroupOutcome(gr GroupResult, asJSON, inGroupOnly, strictExit bool) {
	if inGroupOnly {
		if gr.InGroup {
			fmt.Println("true")
		} else {
			fmt.Println("false")
		}
	} else if asJSON {
		emitJSON(gr)
	} else {
		renderGroup(gr)
	}
	if strictExit {
		if gr.InGroup {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

func normNumber(x string) string {
	// v2.2: 上限由 12 放宽到 20 —— 频道号(guild_id)为 16~18 位数字，
	//       群号/QQ号为 5~12 位，两个区间不重叠，可据此区分类型。
	if !regexp.MustCompile(`^\d{5,20}$`).MatchString(x) {
		die("号码格式不正确（应为 5~20 位数字）: " + x)
	}
	return x
}

// kindByFormat 按位数区间做类型预判（不依赖数据，恒定可用）
//
// 区间依据（2026-10-04 实测本机 3 个空间的频道缓存，样本 70 条）：
//   频道号(guild_id)：16~18 位（16位x2, 17位x18, 18位x50）
//   群号 / QQ号     ：5~12 位
//   13~15、19~20 位 ：未观测到，归为 unknown，不做臆断
func kindByFormat(tok string) string {
	n := len(tok)
	switch {
	case n >= 16 && n <= 18:
		return "channel"
	case n >= 5 && n <= 12:
		return "short"
	default:
		return "unknown"
	}
}

func initTZ() {
	if os.Getenv("TZ") != "" {
		return
	}
	if b, err := os.ReadFile("/data/property/persist.sys.timezone"); err == nil {
		tz := strings.TrimSpace(string(b))
		if tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				time.Local = loc
				return
			}
			if zd, err2 := os.ReadFile("/system/usr/share/zoneinfo/" + tz); err2 == nil {
				if z2, err3 := time.LoadLocationFromTZData(tz, zd); err3 == nil {
					time.Local = z2
					return
				}
			}
		}
	}
	time.Local = time.FixedZone("CST", 8*3600)
}

func realMain() {
	initTZ()
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage()
		return
	}
	asJSON := false
	inGroupOnly := false // [v2.1] 只输出 true/false
	strictExit := false  // [v2.1] 退出码表达"是否在群"
	spaceFilter := "all"
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			asJSON = true
		case a == "--space":
			if i+1 < len(args) {
				spaceFilter = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--space="):
			spaceFilter = strings.SplitN(a, "=", 2)[1]
		case a == "--in-group":
			// [v2.1] 只输出 true/false（给脚本用，零解析）
			inGroupOnly = true
		case a == "--strict-exit":
			// [v2.1] 按"是否在群"设退出码：0=在群 1=不在群 2=错误
			strictExit = true
		case a == "-v" || a == "--version":
			fmt.Println("qqcheck v" + version)
			return
		default:
			rest = append(rest, a)
		}
	}
	spaces = detectSpaces()
	if spaceFilter == "main" {
		var f []Space
		for _, s := range spaces {
			if s.Name == "主空间" {
				f = append(f, s)
			}
		}
		spaces = f
	} else if spaceFilter == "dual" {
		var f []Space
		for _, s := range spaces {
			if s.Name != "主空间" {
				f = append(f, s)
			}
		}
		spaces = f
	}
	if len(spaces) == 0 {
		die("未发现 QQ 数据目录")
	}
	checkRoot()
	cmd := "help"
	if len(rest) > 0 {
		cmd = rest[0]
	}
	switch cmd {
	case "check":
		if len(rest) < 2 {
			die("用法: qqcheck check <号码>")
		}
		tok := normNumber(rest[1])
		gs, us, chs := detectKind(tok)
		fk := kindByFormat(tok)
		if fk == "channel" || len(chs) > 0 {
			// 频道：位数区间 16~18，与群号/QQ号(5~12)不重叠
			if len(chs) > 0 {
				fmt.Printf("判定：该号码是【频道】（依据：%s）\n", strings.Join(firstN(chs, 2), "；"))
			} else {
				fmt.Printf("判定：该号码是【频道】（依据：编号 %d 位，落在频道号区间 16~18）\n", len(tok))
			}
			cr := reportChannel(tok)
			if asJSON {
				emitJSON(cr)
			} else {
				renderChannel(cr)
			}
		} else if len(gs) > 0 && len(us) == 0 {
			fmt.Printf("判定：该号码是【群】（依据：%s）\n", strings.Join(firstN(gs, 2), "；"))
			emitGroupOutcome(reportGroup(tok), asJSON, inGroupOnly, strictExit)
		} else if len(us) > 0 && len(gs) == 0 {
			if inGroupOnly || strictExit {
				die("--in-group/--strict-exit 仅适用于群号")
			}
			fmt.Printf("判定：该号码是【用户】（依据：%s）\n", strings.Join(firstN(us, 2), "；"))
			ur := reportUser(tok)
			if asJSON {
				emitJSON(ur)
			} else {
				renderUser(ur)
			}
		} else if len(gs) > 0 && len(us) > 0 {
			fmt.Println("提示：同时发现群与用户痕迹，分别输出两份报告")
			emitGroupOutcome(reportGroup(tok), asJSON, inGroupOnly, strictExit)
			ur := reportUser(tok)
			if asJSON {
				emitJSON(ur)
			} else {
				renderUser(ur)
			}
		} else {
			tip := "群或QQ号（5~12 位）"
			if fk == "unknown" {
				tip = "无法按位数归类（本地未观测到该区间，13~15 与 19~20 位）"
			}
			fmt.Printf("未在本地数据中发现该号码的痕迹。位数 %d，属于：%s。可尝试: qqcheck search %s\n", len(tok), tip, tok)
		}
	case "group":
		if len(rest) < 2 {
			die("用法: qqcheck group <群号>")
		}
		emitGroupOutcome(reportGroup(normNumber(rest[1])), asJSON, inGroupOnly, strictExit)
	case "user":
		if len(rest) < 2 {
			die("用法: qqcheck user <QQ号>")
		}
		ur := reportUser(normNumber(rest[1]))
		if asJSON {
			emitJSON(ur)
		} else {
			renderUser(ur)
		}
	case "member":
		if len(rest) < 3 {
			die("用法: qqcheck member <群号> <QQ号|uid>")
		}
		g := normNumber(rest[1])
		key := rest[2]
		if !regexp.MustCompile(`^(\d{5,12}|u_[A-Za-z0-9_\-]+)$`).MatchString(key) {
			die("第二个参数应为 QQ号 或 uid（u_开头）")
		}
		mr := reportMember(g, key)
		if asJSON {
			emitJSON(mr)
		} else {
			renderMember(mr)
		}
	case "uid":
		if len(rest) < 2 {
			die("用法: qqcheck uid <QQ号>")
		}
		uin := normNumber(rest[1])
		type mapItem struct {
			Space string `json:"space"`
			UID   string `json:"uid"`
		}
		out := struct {
			QQ   string    `json:"qq"`
			Maps []mapItem `json:"maps"`
		}{QQ: uin}
		for _, sp := range spaces {
			if v, ok := uidMap(sp)[uin]; ok {
				out.Maps = append(out.Maps, mapItem{sp.Name, v})
			}
		}
		if asJSON {
			emitJSON(out)
		} else {
			fmt.Printf("QQ号 %s 的 NT内部UID:\n", uin)
			for _, m := range out.Maps {
				fmt.Printf("  【%s】%s\n", m.Space, m.UID)
			}
			if len(out.Maps) == 0 {
				fmt.Println("  （未找到映射；该号码可能未出现在本地缓存中）")
			}
		}
	case "search":
		if len(rest) < 2 {
			die("用法: qqcheck search <关键词或号码>")
		}
		kw := rest[1]
		for _, sp := range spaces {
			fmt.Printf("【%s】搜索: %s\n", sp.Name, kw)
			pr, mm, dbh := scanNumber(sp, kw)
			anyHit := false
			show := func(cat, label, sub string, hits []Hit) {
				for _, h := range hits {
					anyHit = true
					fmt.Printf("  [%s] %s  (%d处, %s)\n", label, h.Name, h.Count, h.Mtime)
					for _, c := range contexts(rdc(sp.Path+sub+h.Name), kw, 60, 110, 2) {
						fmt.Println("      …" + c + "…")
					}
				}
			}
			show("prefs", "shared_prefs", "/shared_prefs/", pr)
			show("mmkv", "mmkv", "/files/mmkv/", mm)
			show("db", "databases", "/databases/", dbh)
			if !anyHit {
				fmt.Println("  （无命中）")
			}
		}
		fmt.Println()
		fmt.Println("提醒：必须核对上下文，防止子串假阳性（如 blackfriday / sig_feed_id 前缀）。")
	case "overview":
		ov := reportOverview()
		if asJSON {
			emitJSON(ov)
		} else {
			renderOverview(ov)
		}
	default:
		// [v2.1] 未知命令应报错退出（原来是打印用法后 exit=0，调用方会误判"成功"）
		fmt.Println("未知命令: " + cmd)
		usage()
		os.Exit(2)
	}
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[内部错误] %v\n", r)
			os.Exit(1)
		}
	}()
	realMain()
}
