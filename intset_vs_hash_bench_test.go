package main

import (
	"math/rand"
	"strconv"
	"testing"
)

/* intset 二分查找 vs go map 哈希定位 —— 规模对照压测。
 *
 * 目的:验证 set 底层为何在元素数超过阈值(REDIS_SET_MAX_INTSET_ENTRIES=512)后,
 * 由 intset 转成哈希表更划算。
 *   - intset 查找是有序数组二分,复杂度 O(log n),n 越大越慢
 *   - 哈希表查找是 O(1),理论上不随 n 增长
 * 把规模从 64 一路拉到 65536,观察两者的查找耗时如何分化。
 *
 * 三种被测查找:
 *   IntsetBinarySearch —— 真实生产实现 intsetSearch(有序数组二分)
 *   MapInt64           —— map[int64]struct{} 哈希定位(隔离纯算法差异)
 *   MapString          —— map[string]struct{} 哈希定位(贴近真实 set HT 编码)
 *
 * 运行:
 *   go test -run=^$ -bench='BenchmarkLookup' -benchmem
 */

var lookupSizes = []int{64, 256, 512, 1024, 4096, 16384, 65536}

// 防止编译器把查找结果优化掉
var sinkLookupBool bool

// buildSortedVals 造 n 个互不相同且升序的整数(intset 二分查找的前提是有序)
func buildSortedVals(n int) []int64 {
	vals := make([]int64, n)
	for i := 0; i < n; i++ {
		vals[i] = int64(i) * 2
	}
	return vals
}

// buildQueries 预生成命中现有元素的查询序列,避免把随机数开销算进基准
func buildQueries(vals []int64, m int) []int64 {
	r := rand.New(rand.NewSource(1))
	q := make([]int64, m)
	for i := 0; i < m; i++ {
		q[i] = vals[r.Intn(len(vals))]
	}
	return q
}

func BenchmarkLookupIntsetBinarySearch(b *testing.B) {
	for _, n := range lookupSizes {
		vals := buildSortedVals(n)
		// 直接以 INT64 编码装载有序切片,intsetSearch 会在其上做二分
		is := &intset{encoding: INTSET_ENC_INT64, length: n, c64: vals}
		queries := buildQueries(vals, 1024)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			var found bool
			for i := 0; i < b.N; i++ {
				found, _ = intsetSearch(is, queries[i&1023])
			}
			sinkLookupBool = found
		})
	}
}

func BenchmarkLookupMapInt64(b *testing.B) {
	for _, n := range lookupSizes {
		vals := buildSortedVals(n)
		m := make(map[int64]struct{}, n)
		for _, v := range vals {
			m[v] = struct{}{}
		}
		queries := buildQueries(vals, 1024)
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			var found bool
			for i := 0; i < b.N; i++ {
				_, found = m[queries[i&1023]]
			}
			sinkLookupBool = found
		})
	}
}

func BenchmarkLookupMapString(b *testing.B) {
	for _, n := range lookupSizes {
		vals := buildSortedVals(n)
		m := make(map[string]struct{}, n)
		for _, v := range vals {
			m[strconv.FormatInt(v, 10)] = struct{}{}
		}
		qInts := buildQueries(vals, 1024)
		queries := make([]string, len(qInts))
		for i, v := range qInts {
			queries[i] = strconv.FormatInt(v, 10)
		}
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			var found bool
			for i := 0; i < b.N; i++ {
				_, found = m[queries[i&1023]]
			}
			sinkLookupBool = found
		})
	}
}
