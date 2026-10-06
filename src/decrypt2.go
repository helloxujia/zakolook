// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// 自适应解密，支持两种库格式。
//
// nt 系（库头含 "QQ_NT DB"）：
//
//	passphrase = md5(md5(uid) + rand)
//
//	rand 取自库头 protobuf 的字段 2；reserve 由库头声明的 HMAC 算法决定。
//
// ORM 系（库头含 "xxx DB v1.0.0"，例如频道库的 "Guild DB v1.0.0"）：
//
//	K    = md5(md5(qq) + md5(sha1_raw(qq)))
//	pass = md5(md5(hdr[51:67]) + K)
//
//	reserve 固定为 80。
//
// 实现时有几点需要注意：
//
//   - 第 1 页的密文不含 SQLite 魔数，魔数由解密方补回。因此校验页 1 时
//     不能匹配 "SQLite format 3"，而要检查页内偏移 16 处的头字段：
//     页大小 0x1000，以及紧随其后的 64/32/32 三个负载比。
//   - reserve 并非常量：HMAC-SHA1 为 48，HMAC-SHA512 为 80。
//   - 频道库头中的 PRAGMA cipher_use_hmac = OFF 未生效，不能按 16 计算。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Scheme 描述一种可供尝试的解密方案。
type Scheme struct {
	Kind     string
	Pass     string
	Reserve  int
	Rand     string
	Version  string
	HMACAlgo string
}

func md5hexBytes(b []byte) string {
	x := md5.Sum(b)
	return hex.EncodeToString(x[:])
}

func hmacSize(algo string) int {
	switch strings.ToUpper(algo) {
	case "HMAC_SHA1", "SHA1":
		return 20
	case "HMAC_SHA256", "SHA256":
		return 32
	case "HMAC_SHA512", "SHA512":
		return 64
	}
	return 20
}

func reserveForHMAC(sz int) int {
	b := 16 + sz
	return b + ((16 - b%16) % 16)
}

// ormPassphrase 频道库（ORM 系）口令
func ormPassphrase(qq string, hdr []byte) string {
	if len(hdr) < 67 {
		return ""
	}
	s1 := sha1.Sum([]byte(qq))
	k := md5hexBytes([]byte(md5hexBytes([]byte(qq)) + md5hexBytes(s1[:])))
	return md5hexBytes([]byte(md5hexBytes(hdr[51:67]) + k))
}

// verifyPage1 校验第 1 页：解密后开头应为页大小 0x1000（大端）。
func verifyPage1(pt []byte) bool {
	return len(pt) >= 8 &&
		pt[0] == 0x10 && pt[1] == 0x00 &&
		pt[5] == 0x40 && pt[6] == 0x20 && pt[7] == 0x20
}

// schemeCandidates 按可能性排序返回候选方案，并用不同的 reserve 兜底。
func schemeCandidates(hdr []byte, uid, qq string) []Scheme {
	var pri []Scheme
	if h, err := parseHeader(hdr); err == nil && h != nil && h.Rand != "" {
		pri = append(pri, Scheme{Kind: "nt", Pass: derivePassphrase(uid, h.Rand),
			Reserve: reserveForHMAC(hmacSize(h.HMACAlgo)), Rand: h.Rand,
			Version: h.Version, HMACAlgo: h.HMACAlgo})
	}
	if qq != "" && len(hdr) >= 68 {
		tag := strings.TrimRight(string(hdr[32:48]), "\x00")
		if strings.Contains(tag, "DB v") {
			if p := ormPassphrase(qq, hdr); p != "" {
				pri = append(pri, Scheme{Kind: "orm", Pass: p, Reserve: 80})
			}
		}
	}
	var alt []Scheme
	for _, s := range pri {
		for _, r := range []int{48, 80, 16} {
			if r != s.Reserve {
				c := s
				c.Reserve = r
				alt = append(alt, c)
			}
		}
	}
	return append(pri, alt...)
}

func readSalt(f *os.File) ([]byte, error) {
	s := make([]byte, 16)
	if _, err := f.ReadAt(s, ExtHeader); err != nil {
		return nil, err
	}
	return s, nil
}

// decryptWith 按给定方案解密。先校验第 1 页，不通过则直接返回错误，
// 避免输出解密不完整的数据。
func decryptWith(in, out string, sc Scheme) error {
	if sc.Reserve <= 0 || sc.Reserve >= PageSize-16 {
		return errors.New("bad reserve")
	}
	fi, err := os.Open(in)
	if err != nil {
		return err
	}
	defer fi.Close()
	st, err := fi.Stat()
	if err != nil {
		return err
	}
	if st.Size() < ExtHeader+PageSize {
		return errors.New("file too small")
	}
	salt, err := readSalt(fi)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(pbkdf2HMACSHA512([]byte(sc.Pass), salt, 4000, 32))
	if err != nil {
		return err
	}
	dataEnd := PageSize - sc.Reserve

	// 先验第 1 页
	p1 := make([]byte, PageSize)
	if _, err = fi.ReadAt(p1, ExtHeader); err != nil {
		return err
	}
	tmp := make([]byte, dataEnd-16)
	cipher.NewCBCDecrypter(block, p1[dataEnd:dataEnd+16]).CryptBlocks(tmp, p1[16:dataEnd])
	if !verifyPage1(tmp) {
		return fmt.Errorf("page1 校验失败 (reserve=%d)", sc.Reserve)
	}

	// 整库
	total := int((st.Size() - ExtHeader) / PageSize)
	fo, err := os.Create(out)
	if err != nil {
		return err
	}
	defer fo.Close()
	raw := make([]byte, PageSize)
	pt := make([]byte, dataEnd)
	pg := make([]byte, PageSize)
	for pgno := 1; pgno <= total; pgno++ {
		if _, err = fi.ReadAt(raw, int64(ExtHeader+(pgno-1)*PageSize)); err != nil {
			return err
		}
		skip := 0
		if pgno == 1 {
			skip = 16
		}
		ct := raw[skip:dataEnd]
		cipher.NewCBCDecrypter(block, raw[dataEnd:dataEnd+16]).CryptBlocks(pt[:len(ct)], ct)
		for i := range pg {
			pg[i] = 0
		}
		n := len(ct)
		if pgno == 1 {
			copy(pg[0:16], "SQLite format 3\x00")
			copy(pg[16:16+n], pt[:n])
			pg[16] = byte(PageSize >> 8)
			pg[17] = byte(PageSize & 0xFF)
			pg[20] = byte(sc.Reserve)
		} else {
			copy(pg[0:n], pt[:n])
		}
		if _, err = fo.Write(pg); err != nil {
			return err
		}
	}
	return fo.Sync()
}

// decryptFileAuto 自动识别方案并解密文件。
func decryptFileAuto(in, out, uid, qq string) (map[string]string, error) {
	fi, err := os.Open(in)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, ExtHeader)
	_, err = fi.ReadAt(hdr, 0)
	fi.Close()
	if err != nil {
		return nil, err
	}
	cands := schemeCandidates(hdr, uid, qq)
	if len(cands) == 0 {
		return nil, errors.New("未识别的库头（既无 QQ_NT DB 也无 xxx DB vX）")
	}
	var lastErr error
	for _, sc := range cands {
		if err := decryptWith(in, out, sc); err != nil {
			lastErr = err
			continue
		}
		return map[string]string{
			"kind": sc.Kind, "reserve": fmt.Sprint(sc.Reserve), "pass": sc.Pass,
			"rand": sc.Rand, "version": sc.Version, "hmac": sc.HMACAlgo,
		}, nil
	}
	return nil, fmt.Errorf("全部方案校验失败（可能 QQ 版本变更）: %v", lastErr)
}

// mergeWALFile 把 -wal 中已提交的页合并进明文库。
//
// 只应用到"最后一条 commit 帧"（dbsize != 0）为止；第 1 页跳过。
// srcEnc 为对应的加密源库（取 salt 用）。
func mergeWALFile(plainPath, walPath, srcEnc, pass string, reserve int) (int, error) {
	plain, err := os.ReadFile(plainPath)
	if err != nil {
		return 0, err
	}
	w, err := os.ReadFile(walPath)
	if err != nil {
		return 0, err
	}
	if len(w) < 32 || w[0] != 0x37 || w[1] != 0x7f || w[2] != 0x06 || w[3] != 0x82 {
		return 0, errors.New("not a WAL")
	}
	sf, err := os.Open(srcEnc)
	if err != nil {
		return 0, err
	}
	salt, err := readSalt(sf)
	sf.Close()
	if err != nil {
		return 0, err
	}
	block, err := aes.NewCipher(pbkdf2HMACSHA512([]byte(pass), salt, 4000, 32))
	if err != nil {
		return 0, err
	}
	const frame = 24 + PageSize
	n := (len(w) - 32) / frame
	last := -1
	for k := 0; k < n; k++ {
		off := 32 + k*frame
		pgno := int(uint32(w[off])<<24 | uint32(w[off+1])<<16 | uint32(w[off+2])<<8 | uint32(w[off+3]))
		dbsz := int(uint32(w[off+4])<<24 | uint32(w[off+5])<<16 | uint32(w[off+6])<<8 | uint32(w[off+7]))
		if pgno == 0 {
			break
		}
		if dbsz != 0 {
			last = k
		}
	}
	if last < 0 {
		return 0, nil
	}
	dataEnd := PageSize - reserve
	applied := 0
	for k := 0; k <= last; k++ {
		off := 32 + k*frame
		pgno := int(uint32(w[off])<<24 | uint32(w[off+1])<<16 | uint32(w[off+2])<<8 | uint32(w[off+3]))
		if pgno <= 1 || pgno > len(plain)/PageSize {
			continue
		}
		fr := w[off+24 : off+24+PageSize]
		pt := make([]byte, dataEnd)
		cipher.NewCBCDecrypter(block, fr[dataEnd:dataEnd+16]).CryptBlocks(pt, fr[0:dataEnd])
		st := (pgno - 1) * PageSize
		copy(plain[st:st+PageSize], pt)
		applied++
	}
	if err := os.WriteFile(plainPath, plain, 0600); err != nil {
		return 0, err
	}
	return applied, nil
}
