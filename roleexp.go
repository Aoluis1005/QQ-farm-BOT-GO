package main

import (
	"encoding/json"
	_ "embed"
)

//go:embed RoleLevel.json
var roleLevelJSON []byte

type roleLevelEntry struct {
	Level int64 `json:"level"`
	Exp   int64 `json:"exp"`
}

var roleLevelExp = parseRoleLevel()

func parseRoleLevel() map[int64]int64 {
	var arr []roleLevelEntry
	if err := json.Unmarshal(roleLevelJSON, &arr); err != nil {
		return map[int64]int64{}
	}
	m := make(map[int64]int64, len(arr))
	for _, e := range arr {
		m[e.Level] = e.Exp
	}
	return m
}

// expUpperFor 本级经验上限 = 升到下一级所需的累计经验门槛
func expUpperFor(level int64) int64 {
	if v, ok := roleLevelExp[level+1]; ok {
		return v
	}
	return roleLevelExp[level]
}

// expPercentFor 经验进度百分比
func expPercentFor(level, exp int64) int {
	low := roleLevelExp[level]
	up := expUpperFor(level)
	if up <= low {
		return 100
	}
	pct := int((exp - low) * 100 / (up - low))
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}
