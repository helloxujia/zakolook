//go:build sql
// +build sql

package main

// 完整版：使用 modernc.org/sqlite（纯 Go 实现的 SQLite 驱动，无 cgo 依赖）
// 构建方式: go build -tags sql

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

func init() { hasSQL = true }

func dbOpen(path string) *sql.DB {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil
	}
	return db
}

func dbGroupEvidence(cp, g string) GEvid {
	var ev GEvid
	db := dbOpen(cp)
	if db == nil {
		return ev
	}
	defer db.Close()
	t := "TroopFileTansferItemEntity" + g
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", t).Scan(&n); err == nil && n > 0 {
		ev.FileTable = true
		var mn, mx sql.NullInt64
		_ = db.QueryRow("SELECT COUNT(*), MIN(CASE WHEN UploadTime>0 THEN UploadTime END), MAX(CASE WHEN UploadTime>0 THEN UploadTime END) FROM \"" + t + "\"").Scan(&ev.FileTableN, &mn, &mx)
		ev.FileTableMin = mn.Int64
		ev.FileTableMax = mx.Int64
	}
	var mn2, mx2 sql.NullInt64
	if err := db.QueryRow("SELECT COUNT(*), MIN(CASE WHEN srvTime>0 THEN srvTime END), MAX(CASE WHEN srvTime>0 THEN srvTime END) FROM mr_fileManager WHERE CAST(TroopUin AS TEXT)=?", g).Scan(&ev.FileMgrN, &mn2, &mx2); err == nil {
		ev.FileMgrMin = mn2.Int64
		ev.FileMgrMax = mx2.Int64
	}
	var mx3 sql.NullInt64
	if err := db.QueryRow("SELECT COUNT(*), MAX(time) FROM TroopNotificationCache WHERE CAST(troopUin AS TEXT)=?", g).Scan(&ev.NotifN, &mx3); err == nil {
		ev.NotifMax = mx3.Int64
	}
	var mx4 sql.NullInt64
	if err := db.QueryRow("SELECT COUNT(*), MAX(opTime) FROM TroopEssenceMsgItem WHERE CAST(troopUin AS TEXT)=?", g).Scan(&ev.EssN, &mx4); err == nil {
		ev.EssMax = mx4.Int64
	}
	return ev
}

func dbGroupsWithFiles(cp string) []string {
	var out []string
	db := dbOpen(cp)
	if db == nil {
		return out
	}
	defer db.Close()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE name LIKE 'TroopFileTansferItemEntity%'")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, strings.TrimPrefix(n, "TroopFileTansferItemEntity"))
		}
	}
	return out
}
