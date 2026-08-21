package proto

import "testing"

// 构造生涯统计响应并校验原始解码
func buildTestCareerReply() []byte {
	b := NewBuilder()

	// field 1: CareerStatItem {f1=40013, f2=50}
	it := NewBuilder()
	it.FieldInt64(1, 40013)
	it.FieldInt64Always(2, 50)
	b.FieldMessage(1, it.Bytes())

	b.FieldInt64(2, 123) // stats_total
	b.FieldInt64(3, 7)   // stats_count
	b.FieldString(4, "TestName")
	b.FieldString(5, "https://avatar.example/a.png")
	b.FieldInt64(9, 12)    // level
	b.FieldInt64(10, 1000) // exp
	b.FieldInt64(11, 999)  // gid

	// field 12: CareerLevelStat {f1=40001, f2=30, f4=5}
	ls := NewBuilder()
	ls.FieldInt64(1, 40001)
	ls.FieldInt64(2, 30)
	ls.FieldInt64(4, 5)
	b.FieldMessage(12, ls.Bytes())

	b.FieldInt64(13, 8) // achieved_levels
	b.FieldString(15, "openid123")

	return b.Bytes()
}

func TestDecodeCareerInfoGetReply(t *testing.T) {
	rep := DecodeCareerInfoGetReply(buildTestCareerReply())
	if rep.StatsTotal != 123 {
		t.Fatalf("StatsTotal=%d want 123", rep.StatsTotal)
	}
	if rep.StatsCount != 7 {
		t.Fatalf("StatsCount=%d want 7", rep.StatsCount)
	}
	if rep.Name != "TestName" {
		t.Fatalf("Name=%q want TestName", rep.Name)
	}
	if rep.Avatar != "https://avatar.example/a.png" {
		t.Fatalf("Avatar=%q", rep.Avatar)
	}
	if rep.Level != 12 || rep.Exp != 1000 || rep.GID != 999 {
		t.Fatalf("level/exp/gid=%d/%d/%d", rep.Level, rep.Exp, rep.GID)
	}
	if rep.AchievedLevels != 8 {
		t.Fatalf("AchievedLevels=%d want 8", rep.AchievedLevels)
	}
	if rep.OpenID != "openid123" {
		t.Fatalf("OpenID=%q", rep.OpenID)
	}
	if len(rep.Items) != 1 {
		t.Fatalf("items len=%d want 1", len(rep.Items))
	}
	item := rep.Items[0]
	if item.FruitID != 40013 || item.Count != 50 {
		t.Fatalf("item=%+v", item)
	}
	if len(rep.LevelStats) != 1 {
		t.Fatalf("levelStats len=%d want 1", len(rep.LevelStats))
	}
	ls := rep.LevelStats[0]
	if ls.FruitID != 40001 || ls.Count != 30 || ls.Level != 5 {
		t.Fatalf("levelStat=%+v", ls)
	}
}

func TestPrintableString(t *testing.T) {
	if printableString([]byte("")) != "" {
		t.Fatal("empty should be empty")
	}
	// 中文（非 ASCII）应被丢弃，
	if printableString([]byte("玩家")) != "" {
		t.Fatal("non-ascii should be empty")
	}
	if printableString([]byte("https://a/b.png")) != "https://a/b.png" {
		t.Fatal("ascii should pass")
	}
}
