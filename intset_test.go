package main

import (
	"testing"
)

// newIntsetForTest 构造一个空 intset,便于测试
func newIntsetForTest() *intset {
	return &intset{encoding: INTSET_ENC_INT16}
}

// intsetSnapshot 返回当前内容快照(应保持升序);
// 经 intsetGet 统一以 int64 取出,屏蔽底层 c16/c32/c64 的位宽差异。
func intsetSnapshot(is *intset) []int64 {
	out := make([]int64, is.length)
	for i := 0; i < is.length; i++ {
		out[i] = intsetGet(is, i)
	}
	return out
}

// --- intsetValueEncoding ---

func TestIntsetValueEncoding_Basic(t *testing.T) {
	cases := []struct {
		v   int64
		enc int
	}{
		{0, INTSET_ENC_INT16},
		{32767, INTSET_ENC_INT16},                // max int16
		{-32768, INTSET_ENC_INT16},               // min int16
		{32768, INTSET_ENC_INT32},                // 超出 int16 → int32
		{2147483647, INTSET_ENC_INT32},           // max int32
		{-2147483648, INTSET_ENC_INT32},          // min int32
		{2147483648, INTSET_ENC_INT64},           // 超出 int32 → int64
		{9223372036854775807, INTSET_ENC_INT64},  // max int64
		{-9223372036854775808, INTSET_ENC_INT64}, // min int64
	}
	for _, c := range cases {
		if got := intsetValueEncoding(c.v); got != c.enc {
			t.Errorf("intsetValueEncoding(%d) = %d, want %d", c.v, got, c.enc)
		}
	}
}

// --- intsetSearch: 回归测试,锁定此前修复的两个 off-by-one ---

// 回归 Bug1: 单元素集合命中应返回 found=true(循环条件 <=)
func TestIntsetSearch_SingleElementHit(t *testing.T) {
	is := newIntsetForTest()
	is.c16 = []int16{5}
	is.length = 1

	found, pos := intsetSearch(is, 5)
	if !found || pos != 0 {
		t.Errorf("search(5) in [5] = (%v,%d), want (true,0) — 去重依赖此命中", found, pos)
	}
}

// 回归 Bug1: 末位元素命中
func TestIntsetSearch_LastElementHit(t *testing.T) {
	is := newIntsetForTest()
	is.c16 = []int16{1, 3, 5, 7}
	is.length = 4

	found, pos := intsetSearch(is, 7)
	if !found || pos != 3 {
		t.Errorf("search(7) in [1,3,5,7] = (%v,%d), want (true,3)", found, pos)
	}
}

// 回归 Bug2: 插入小于最小值,应返回 pos=0(返回 lo 而非 stale mid)
func TestIntsetSearch_InsertBeforeMin(t *testing.T) {
	is := newIntsetForTest()
	is.c16 = []int16{1, 3, 5, 7}
	is.length = 4

	found, pos := intsetSearch(is, 0)
	if found || pos != 0 {
		t.Errorf("search(0) in [1,3,5,7] = (%v,%d), want (false,0) — 否则顺序被破坏", found, pos)
	}
}

// 插入中间位置
func TestIntsetSearch_InsertMiddle(t *testing.T) {
	is := newIntsetForTest()
	is.c16 = []int16{1, 3, 5, 7}
	is.length = 4

	found, pos := intsetSearch(is, 4)
	if found || pos != 2 {
		t.Errorf("search(4) in [1,3,5,7] = (%v,%d), want (false,2)", found, pos)
	}
}

// --- intsetAdd: 保持升序 + 幂等去重 ---

func TestIntsetAdd_Basic(t *testing.T) {
	is := newIntsetForTest()
	// 乱序插入
	for _, v := range []int64{7, 1, 5, 3} {
		intsetAdd(is, v)
	}
	got := intsetSnapshot(is)
	want := []int64{1, 3, 5, 7}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("contents[%d] = %d, want %d (升序被破坏)", i, got[i], want[i])
		}
	}
}

// 重复添加同一元素判断是否幂等处理(返回 0,集合不变)
func TestIntsetAdd_Dedup(t *testing.T) {
	is := newIntsetForTest()
	//重复添加两次5,断言第二次输出0
	if intsetAdd(is, 5) != 1 {
		t.Error("首次添加应返回 1")
	}
	if intsetAdd(is, 5) != 0 {
		t.Error("重复添加应返回 0(幂等)")
	}
	// 通过set数量终态判断去重是否生效
	if is.length != 1 {
		t.Errorf("去重后 length = %d, want 1", is.length)
	}
}

// 插入触发位宽升级(int16 → int32 → int64)
func TestIntsetAdd_EncodingUpgrade(t *testing.T) {
	is := newIntsetForTest()
	intsetAdd(is, 1) // INT16
	if is.encoding != INTSET_ENC_INT16 {
		t.Errorf("encoding = %d, want INT16", is.encoding)
	}
	intsetAdd(is, 40000) // 超出 int16 → INT32
	if is.encoding != INTSET_ENC_INT32 {
		t.Errorf("encoding = %d, want INT32", is.encoding)
	}
	intsetAdd(is, 3000000000) // 超出 int32 → INT64
	if is.encoding != INTSET_ENC_INT64 {
		t.Errorf("encoding = %d, want INT64", is.encoding)
	}
}

// 测试删除元素
func TestIntsetRemove_Basic(t *testing.T) {
	is := newIntsetForTest()
	//插入元素
	for _, v := range []int64{1, 3, 5, 7} {
		intsetAdd(is, v)
	}
	//移除3 判断返回索引是否为1
	if intsetRemove(is, 3) != 1 {
		t.Error("删除存在的元素应返回 1")
	}
	//创建快照单元测试,比对移除结果是否准确
	got := intsetSnapshot(is)
	want := []int64{1, 5, 7}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("after remove(3): contents[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	//删除不存在的元素,覆盖边界
	if intsetRemove(is, 100) != 0 {
		t.Error("删除不存在的元素应返回 0")
	}
}
