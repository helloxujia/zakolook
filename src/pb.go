// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// 40800 消息体解析（schema 驱动的最小实现）
//
// 字段号来源：schema/ntqq-9.9.27.45758.json
//   40800 = body_content(段)   45001 = 段 msg_id   45002 = content_type
//   45101 = 文本               45402 = 文件名      47413 = 引用摘要
//   48271 = 系统消息 JSON（本机实测补充）

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

const (
	fBodyContent = 40800
	fSegMsgID    = 45001
	fSegType     = 45002
	fText        = 45101
	fFileName    = 45402
	fRefSummary  = 47413
	fSysJSON     = 48271
)

var kindMap = map[int]string{
	1: "text", 2: "image", 3: "file", 4: "audio", 5: "video",
	6: "sticker", 7: "reply", 8: "system", 10: "other", 11: "forward", 16: "mixed",
}

// pbFields 解析 protobuf wire-format，返回 (字段号, wire, 值)
func pbFields(d []byte) (out []struct {
	No    int
	Wire  int
	Int   uint64
	Bytes []byte
}, err error) {
	i := 0
	for i < len(d) {
		var tag uint64
		var sh uint
		for {
			if i >= len(d) || sh > 63 {
				return out, errBadVarint
			}
			b := d[i]
			i++
			tag |= uint64(b&0x7f) << sh
			if b < 0x80 {
				break
			}
			sh += 7
		}
		no, wt := int(tag>>3), int(tag&7)
		if no == 0 {
			return out, errFieldZero
		}
		switch wt {
		case 0:
			var v uint64
			var sh2 uint
			for {
				if i >= len(d) || sh2 > 63 {
					return out, errBadVarint
				}
				b := d[i]
				i++
				v |= uint64(b&0x7f) << sh2
				if b < 0x80 {
					break
				}
				sh2 += 7
			}
			out = append(out, struct {
				No    int
				Wire  int
				Int   uint64
				Bytes []byte
			}{no, 0, v, nil})
		case 2:
			var l uint64
			var sh2 uint
			for {
				if i >= len(d) || sh2 > 63 {
					return out, errBadVarint
				}
				b := d[i]
				i++
				l |= uint64(b&0x7f) << sh2
				if b < 0x80 {
					break
				}
				sh2 += 7
			}
			if i+int(l) > len(d) {
				return out, errTruncated
			}
			out = append(out, struct {
				No    int
				Wire  int
				Int   uint64
				Bytes []byte
			}{no, 2, 0, d[i : i+int(l)]})
			i += int(l)
		case 5:
			if i+4 > len(d) {
				return out, errTruncated
			}
			i += 4
		case 1:
			if i+8 > len(d) {
				return out, errTruncated
			}
			i += 8
		default:
			return out, errWireType
		}
	}
	return out, nil
}

var (
	errBadVarint = errStr("bad varint")
	errFieldZero = errStr("field 0")
	errTruncated = errStr("truncated")
	errWireType  = errStr("wire type")
)

type errStr string

func (e errStr) Error() string { return string(e) }

// decodeBody 解析 40800 blob → (kind, text)
func decodeBody(blob []byte) (string, string) {
	if len(blob) == 0 {
		return "empty", ""
	}
	top, err := pbFields(blob)
	if err != nil {
		return "other", ""
	}
	var texts []string
	kinds := map[string]bool{}
	for _, f := range top {
		if f.No != fBodyContent || f.Wire != 2 {
			continue
		}
		segs, err := pbFields(f.Bytes)
		if err != nil {
			continue
		}
		ct, text, sysJSON, fname := 0, "", []byte(nil), ""
		for _, s := range segs {
			switch s.No {
			case fSegType:
				ct = int(s.Int)
			case fText:
				text = string(s.Bytes)
			case fSysJSON:
				sysJSON = s.Bytes
			case fFileName:
				fname = string(s.Bytes)
			case fRefSummary:
				if text == "" {
					text = string(s.Bytes)
				}
			}
		}
		k := kindMap[ct]
		if k == "" {
			k = "other"
		}
		kinds[k] = true
		if text == "" && len(sysJSON) > 0 {
			text = sysJSONText(sysJSON)
		}
		if text != "" {
			texts = append(texts, trimText(text))
		} else if fname != "" {
			texts = append(texts, "["+fname+"]")
		}
	}
	txt := joinTexts(texts)
	for _, p := range []string{"text", "reply", "forward", "file", "video", "image", "sticker", "audio", "system"} {
		if kinds[p] {
			return p, txt
		}
	}
	if len(kinds) > 0 {
		for k := range kinds {
			return k, txt
		}
	}
	return "other", txt
}

// sysJSONText 从系统消息 JSON（48271）提取可读文本
func sysJSONText(b []byte) string {
	var v any
	if err := json.Unmarshal(bytes.TrimSpace(b), &v); err != nil {
		return ""
	}
	var parts []string
	var walk func(any)
	walk = func(o any) {
		switch t := o.(type) {
		case map[string]any:
			for k, vv := range t {
				switch k {
				case "nm", "txt", "text":
					if s, ok := vv.(string); ok && s != "" {
						parts = append(parts, s)
					}
				case "uin", "jp":
					switch x := vv.(type) {
					case string:
						parts = append(parts, "["+x+"]")
					case float64:
						parts = append(parts, "["+strconv.FormatInt(int64(x), 10)+"]")
					}
				default:
					walk(vv)
				}
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(v)
	if len(parts) == 0 {
		return ""
	}
	return joinTexts(parts)
}

func trimText(s string) string {
	// 折叠空白，去除控制字符
	var b []rune
	prevSpace := false
	for _, r := range s {
		if r < 32 {
			if !prevSpace {
				b = append(b, ' ')
				prevSpace = true
			}
			continue
		}
		if r == ' ' {
			if !prevSpace {
				b = append(b, r)
				prevSpace = true
			}
			continue
		}
		b = append(b, r)
		prevSpace = false
	}
	out := string(b)
	for len(out) > 0 && out[0] == ' ' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}

func joinTexts(ss []string) string {
	out := ""
	for i, s := range ss {
		if s == "" {
			continue
		}
		if i > 0 && out != "" {
			out += " | "
		}
		out += s
	}
	return out
}

func timeNow() int64 { return time.Now().Unix() }

// timeFull 带年份的日期（用于跨度类展示，如入群时间）
func timeFull(ts int64) string {
	return time.Unix(ts, 0).Format("2006-01-02")
}

func timeStr(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02 15:04")
}
