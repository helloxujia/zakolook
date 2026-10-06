// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 xujia2024
// Project: https://gitee.com/xujia2024/zakolook
// Mirror:  https://github.com/helloxujia/zakolook
// See LICENSE for the additional terms under section 7.

package main

// NTQQ 数据库解密核心（无外部依赖：PBKDF2 自实现）
// 参数依据 schema/ntqq-9.9.27.45758.json：
//   page 4096, crypto_reserve 48, HMAC-SHA1, PBKDF2-HMAC-SHA512(iter=4000,key=32), WAL

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	ExtHeader  = 1024
	PageSize   = 4096
	CryptoResv = 48
)

// pbkdf2HMACSHA512 自实现（避免引入 x/crypto）
func pbkdf2HMACSHA512(password, salt []byte, iter, keyLen int) []byte {
	h := hmac.New(sha512.New, password)
	hashLen := h.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var dk []byte
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		h.Reset()
		h.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		h.Write(buf)
		t := h.Sum(nil)
		u := make([]byte, len(t))
		copy(u, t)
		for i := 1; i < iter; i++ {
			h.Reset()
			h.Write(u)
			u = h.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func md5hex(s string) string {
	x := md5.Sum([]byte(s))
	return hex.EncodeToString(x[:])
}

// NTHeader 私有头字段
type NTHeader struct {
	Rand      string
	Version   string
	HMACAlgo  string
	Timestamp uint64
}

// parseHeader 解析 1024 字节私有头（protobuf 变体）
func parseHeader(d []byte) (*NTHeader, error) {
	if len(d) < ExtHeader {
		return nil, errors.New("header too small")
	}
	i := strings.Index(string(d[:ExtHeader]), "QQ_NT DB")
	if i < 0 {
		return nil, errors.New("magic not found")
	}
	base := i + len("QQ_NT DB")
	ln := int(uint32(d[base]) | uint32(d[base+1])<<8 | uint32(d[base+2])<<16 | uint32(d[base+3])<<24)
	pb := d[base+4 : base+4+ln]
	h := &NTHeader{}
	p := 0
	rv := func() (uint64, error) {
		var v uint64
		var s uint
		for p < len(pb) {
			b := pb[p]
			p++
			v |= uint64(b&0x7f) << s
			if b < 0x80 {
				return v, nil
			}
			s += 7
		}
		return 0, errors.New("bad varint")
	}
	for p < len(pb) {
		tag, err := rv()
		if err != nil {
			break
		}
		field, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			v, _ := rv()
			if field == 5 {
				h.Timestamp = v
			}
		case 2:
			l, _ := rv()
			if p+int(l) > len(pb) {
				return h, nil
			}
			val := pb[p : p+int(l)]
			p += int(l)
			switch field {
			case 2:
				h.Rand = string(val)
			case 3:
				h.Version = string(val)
			case 4:
				h.HMACAlgo = string(val)
			}
		default:
			return h, nil
		}
	}
	return h, nil
}

// derivePassphrase passphrase = md5(md5(uid) + rand)
func derivePassphrase(uid, rand string) string {
	return md5hex(md5hex(uid) + rand)
}

// decryptFile 解密 NTQQ 库到明文文件（流式，内存峰值 ~ 2 页）
//
// 相比一次性读入的方式，本实现逐页 ReadAt/Write，
// 峰值内存 = 私有头(1KB) + 2×页(8KB)，与库大小无关。
func decryptFile(in, out, uid string) (map[string]string, error) {
	fi, err := os.Open(in)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	var st os.FileInfo
	if st, err = fi.Stat(); err != nil {
		return nil, err
	}
	if st.Size() < ExtHeader+PageSize {
		return nil, errors.New("file too small")
	}

	// 私有头
	hdr := make([]byte, ExtHeader)
	if _, err = fi.ReadAt(hdr, 0); err != nil {
		return nil, err
	}
	hd, err := parseHeader(hdr)
	if err != nil {
		return nil, err
	}
	if hd.Rand == "" {
		return nil, errors.New("rand not found in header")
	}
	pass := derivePassphrase(uid, hd.Rand)

	// salt = 第 1 页前 16 字节
	salt := make([]byte, 16)
	if _, err = fi.ReadAt(salt, ExtHeader); err != nil {
		return nil, err
	}
	encKey := pbkdf2HMACSHA512([]byte(pass), salt, 4000, 32)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}

	total := int((st.Size() - ExtHeader) / PageSize)
	fo, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer fo.Close()

	dataEnd := PageSize - CryptoResv
	raw := make([]byte, PageSize) // 复用缓冲
	pt := make([]byte, dataEnd)   // 复用缓冲
	pg := make([]byte, PageSize)  // 输出页

	for pgno := 1; pgno <= total; pgno++ {
		off := int64(ExtHeader + (pgno-1)*PageSize)
		if _, err = fi.ReadAt(raw, off); err != nil {
			return nil, err
		}
		skip := 0
		if pgno == 1 {
			skip = 16
		}
		ct := raw[skip:dataEnd]
		iv := raw[dataEnd : dataEnd+16]
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt[:len(ct)], ct)

		// 输出页组装：
		//   第 1 页 = [魔数16][解密数据(4032)][零填充48]
		//   第 N 页 = [解密数据(4048)][零填充48]
		n := len(ct) // 4032(第1页) 或 4048(其余页)
		for i := range pg {
			pg[i] = 0
		}
		if pgno == 1 {
			copy(pg[0:16], "SQLite format 3\x00")
			copy(pg[16:16+n], pt[:n])
			pg[16] = byte(PageSize >> 8)   // 页面大小(大端高)
			pg[17] = byte(PageSize & 0xFF) // 页面大小(大端低)
			pg[20] = CryptoResv            // 每页保留字节数
		} else {
			copy(pg[0:n], pt[:n])
		}
		if _, err = fo.Write(pg); err != nil {
			return nil, err
		}
	}
	if err = fo.Sync(); err != nil {
		return nil, err
	}
	return map[string]string{
		"rand": hd.Rand, "version": hd.Version, "hmac": hd.HMACAlgo,
		"passphrase": pass, "pages": fmt.Sprint(total),
		"salt": hex.EncodeToString(salt),
	}, nil
}

// hmacPage1 校验第 1 页（判断 uid/rand 是否正确）
func hmacPage1(raw []byte, pass string) bool {
	if len(raw) < ExtHeader+PageSize {
		return false
	}
	body := raw[ExtHeader:]
	salt := body[:16]
	p1 := body[:PageSize]
	encKey := pbkdf2HMACSHA512([]byte(pass), salt, 4000, 32)
	hmacKey := pbkdf2HMACSHA512(encKey, xorByte(salt, 0x3A), 2, 32)
	end := PageSize - CryptoResv
	ct := p1[16:end]
	iv := p1[end : end+16]
	stored := p1[end+16 : end+16+20]
	m := hmac.New(sha1.New, hmacKey)
	m.Write(ct)
	m.Write(iv)
	m.Write([]byte{1, 0, 0, 0})
	return hmac.Equal(m.Sum(nil), stored)
}

func xorByte(b []byte, x byte) []byte {
	o := make([]byte, len(b))
	for i := range b {
		o[i] = b[i] ^ x
	}
	return o
}
