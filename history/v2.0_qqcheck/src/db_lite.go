//go:build !sql
// +build !sql

package main

// lite 版：不依赖任何外部模块，保证在任何环境可编译。
// SQL 统计项（文件记录条数/时间范围）不可用，其余功能完整。

func dbGroupEvidence(cp, g string) GEvid { return GEvid{} }

func dbGroupsWithFiles(cp string) []string { return nil }
