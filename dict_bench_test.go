package main

import (
	"strconv"
	"testing"
)

// BenchmarkSet 对比用自实现 dict 与 Go map 实现 set 的性能，一次运行直观对照。
// set 只关心成员是否存在，故 value 为空：dict 存 nil，map 用 struct{}{}。
// 运行：go test -run=^$ -bench=BenchmarkSet -benchmem
func BenchmarkSet(b *testing.B) {
	const n = 100000

	// SADD：测试dict和go map写入性能
	b.Run("Add/dict", func(b *testing.B) {

		for i := 0; i < b.N; i++ {
			d := dictCreate(&dbDictType, nil)
			for j := 0; j < n; j++ {
				k := "m:" + strconv.Itoa(j)
				dictAdd(d, createStringObject(&k, len(k)), nil)
			}
		}
	})
	b.Run("Add/gomap", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := make(map[string]struct{})
			for j := 0; j < n; j++ {
				m["m:"+strconv.Itoa(j)] = struct{}{}
			}
		}
	})

	d := dictCreate(&dbDictType, nil)
	m := make(map[string]struct{}, n)
	for j := 0; j < n; j++ {
		k := "m:" + strconv.Itoa(j)
		dictAdd(d, createStringObject(&k, len(k)), nil)
		m[k] = struct{}{}
	}
	// 测试 dict和go map键值对检索性能
	b.Run("IsMember/dict", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if dictFind(d, "m:"+strconv.Itoa(i%n)) == nil {
				b.Fatal("miss")
			}
		}
	})
	b.Run("IsMember/gomap", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, ok := m["m:"+strconv.Itoa(i%n)]; !ok {
				b.Fatal("miss")
			}
		}
	})

	// 测试dict和go map迭代性能
	b.Run("Iterate/dict", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cnt := 0
			it := dictGetIterator(d)
			//需逐桶遍历,跨指针跳跃,cache miss概率高,性能表现差
			for de := dictNext(it); de != nil; de = dictNext(it) {
				cnt++
			}
			dictReleaseIterator(it)
			if cnt != n {
				b.Fatalf("want %d got %d", n, cnt)
			}
		}
	})
	b.Run("Iterate/gomap", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cnt := 0
			//go map桶内key value连续存储,cache line友好
			for range m {
				cnt++
			}
			if cnt != n {
				b.Fatalf("want %d got %d", n, cnt)
			}
		}
	})
}
