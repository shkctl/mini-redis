package main

import (
	"fmt"
	"log"
	"strconv"
	"testing"
)

func TestDictCreate(t *testing.T) {
	d := dictCreate(&dbDictType, nil)

	ht := d.ht

	if ht[0].table != nil || ht[1].table != nil {
		log.Fatal("table is not nil")
	}
	if ht[0].size != 0 || ht[1].size != 0 {
		log.Fatal("size is not 0")
	}
	if ht[0].used != 0 || ht[1].used != 0 {
		log.Fatal("used is not 0")
	}
	if ht[0].sizemask != 0 || ht[1].sizemask != 0 {
		log.Fatal("sizemask is not 0")
	}

	if d.rehashidx != -1 {
		log.Fatal("rehashidx is not -1")
	}

	if d.iterators != 0 {
		log.Fatal("iterators is not 0")
	}

}

func TestDictAdd(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	k := "hello"
	v := "mini-redis"

	dictAdd(d, createStringObject(&k, len(k)), createStringObject(&v, len(v)))
	entry := dictFind(d, k)

	ptr := entry.key.ptr
	s := (*ptr).(string)
	if s != "hello" {
		log.Fatal("key is not hello")
	}

	k1 := "key-1"
	v1 := "value-1"
	dictAdd(d, createStringObject(&k1, len(k1)), createStringObject(&v1, len(v1)))
	entry1 := dictFind(d, k1)
	ptr1 := entry1.key.ptr
	s1 := (*ptr1).(string)
	if s1 != "key-1" {
		log.Fatal("key is not key-1")
	}

}

func TestSameBucketInsertion(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	bucketMap := createHashBucketMap()

	count := 0

	for _, v := range (*bucketMap)[0] {
		dictAdd(d, createStringObject(&v, len(v)), nil)
		count++
		if count > 4 {
			break
		}
	}

	ht := d.ht[0]

	entries := (*ht.table)[0]
	for entries != nil {
		fmt.Println((*entries.key.ptr).(string))
		entries = entries.next
	}
	k := "9"
	find := dictFind(d, k)
	if find == nil {
		log.Fatal("key is not find ")
	}
}

func createHashBucketMap() *map[int][]string {
	sizemask := 3
	m := make(map[int][]string)
	for i := 0; i < 100; i++ {
		idx := dictSdsHash(strconv.Itoa(i)) & sizemask
		m[idx] = append(m[idx], strconv.Itoa(i))
	}
	for key, values := range m {
		fmt.Printf("%d: %v\n", key, len(values))
	}
	return &m
}

func TestDictReplace(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	k := "hello"
	v := "mini-redis"
	dictAdd(d, createStringObject(&k, len(k)), createStringObject(&v, len(v)))

	k1 := "hello"
	v1 := "sharkchili"
	replace := dictReplace(d, createStringObject(&k1, len(k1)), createStringObject(&v1, len(v1)))

	if !replace {
		log.Fatal("replace fail")
	}

}

func TestDictDelete(t *testing.T) {

	d := dictCreate(&dbDictType, nil)
	bucketMap := createHashBucketMap()

	count := 0

	for _, v := range (*bucketMap)[0] {
		dictAdd(d, createStringObject(&v, len(v)), nil)
		count++
		if count == 3 {
			break
		}
	}

	ht := &d.ht[0]
	printEntry((*ht.table)[0])

	//17 12 9 都可以进行一次删除操作
	dictDelete(d, "9")
	printEntry((*ht.table)[0])

	if ht.used != 2 {
		log.Fatal("delete fail ")
	}
	if dictDelete(d, "123123") != DICT_ERR {
		log.Fatal("delete fail ")
	}

}

func printEntry(entries *dictEntry) {
	fmt.Println("printEntry")
	for entries != nil {
		fmt.Println((*entries.key.ptr).(string))
		entries = entries.next
	}
}

func TestDictRehash(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	bucketMap := createHashBucketMap()

	count := 0

	for _, v := range (*bucketMap)[0] {
		dictAdd(d, createStringObject(&v, len(v)), nil)
		count++
		if count == 4 {
			break
		}
	}

	//sizemask := 7
	//m := make(map[int][]string)
	//for i := 0; i < 10000; i++ {
	//	idx := dictSdsHash(strconv.Itoa(i)) & sizemask
	//	m[idx] = append(m[idx], strconv.Itoa(i))
	//}
	//for key, values := range m {
	//	fmt.Printf("%d: %v\n", key, len(values))
	//}
	k := "1000"
	dictAdd(d, createStringObject(&k, len(k)), nil)

	//查看19 17 12 9 1000是否存在
	if dictFind(d, "19") == nil ||
		dictFind(d, "17") == nil ||
		dictFind(d, "12") == nil ||
		dictFind(d, "9") == nil ||
		dictFind(d, "1000") == nil {
		log.Fatal("rehash fail")
	}
}

// TestRev 验证 rev 位反转的关键性质:rev(rev(v)) == v
func TestRev(t *testing.T) {
	cases := []uint64{0, 1, 2, 0xff, 0xdeadbeef, ^uint64(0)}
	for _, v := range cases {
		got := rev(rev(v))
		if got != v {
			t.Fatalf("rev(rev(%x)) = %x, want %x", v, got, v)
		}
	}
	// 单步反向递增应能枚举所有 4 位低位组合(共 16 个不重复值)
	mask := uint64(0xf)
	seen := map[uint64]bool{}
	v := uint64(0)
	for step := 0; step < 32; step++ {
		seen[v&mask] = true
		v |= ^mask
		v = rev(v)
		v++
		v = rev(v)
		if v == 0 {
			break
		}
	}
	if len(seen) != 16 {
		t.Fatalf("expected 16 distinct low-4-bit cursors, got %d", len(seen))
	}
}

// TestDictScanEmpty 验证空字典立即返回 cursor=0
func TestDictScanEmpty(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	visited := 0
	cursor := dictScan(d, 0, func(privdata interface{}, de *dictEntry) {
		visited++
	}, nil)
	if cursor != 0 {
		t.Fatalf("empty dict scan should return cursor=0, got %d", cursor)
	}
	if visited != 0 {
		t.Fatalf("empty dict scan should not visit any entry, got %d", visited)
	}
}

// TestDictScanFullSweep 验证全量遍历不漏 key
// 标准用法:cursor 从 0 开始,反复调用直到再次返回 0
func TestDictScanFullSweep(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	const total = 200
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("key:%d", i)
		val := fmt.Sprintf("val:%d", i)
		dictAdd(d, createStringObject(&key, len(key)), createStringObject(&val, len(val)))
		want[key] = true
	}

	visited := map[string]int{}
	collect := func(privdata interface{}, de *dictEntry) {
		k := (*de.key.ptr).(string)
		visited[k]++
	}

	var cursor uint64
	iterations := 0
	for {
		cursor = dictScan(d, cursor, collect, nil)
		iterations++
		if cursor == 0 {
			break
		}
		if iterations > total*10 {
			t.Fatalf("dictScan did not terminate within %d iterations", total*10)
		}
	}

	// 不漏 key
	for k := range want {
		if visited[k] == 0 {
			t.Fatalf("key %q not visited", k)
		}
	}
	// 遍历不出范围(每个 key 来自原集合)
	for k := range visited {
		if !want[k] {
			t.Fatalf("visited unexpected key %q", k)
		}
	}
}

// TestDictScanDuringRehash 验证 rehash 进行中 dictScan 仍能全量遍历
func TestDictScanDuringRehash(t *testing.T) {
	d := dictCreate(&dbDictType, nil)
	const total = 64
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("rh:%d", i)
		dictAdd(d, createStringObject(&key, len(key)), nil)
		want[key] = true
	}
	// 强制触发扩容进入 rehash:再插入一个新 key 会触发 _dictExpandIfNeeded
	probe := "rh:probe"
	dictAdd(d, createStringObject(&probe, len(probe)), nil)
	want[probe] = true

	// 此时若 ht[1] 已分配,rehashidx != -1,扫描必须能看见所有 key
	visited := map[string]bool{}
	collect := func(privdata interface{}, de *dictEntry) {
		visited[(*de.key.ptr).(string)] = true
	}

	var cursor uint64
	for {
		cursor = dictScan(d, cursor, collect, nil)
		if cursor == 0 {
			break
		}
	}

	for k := range want {
		if !visited[k] {
			t.Fatalf("rehash sweep missed key %q", k)
		}
	}
}

// TestDictScanViaSetCommand 复现 redis-cli SCAN 路径:通过完整的 setupTest + dbAdd 链路灌入 70+ key,
// 验证 SCAN COUNT 10000 一次扫完时能取到所有 key
func TestDictScanViaSetCommand(t *testing.T) {
	setupTest()
	db := &server.db[0]

	const total = 70
	want := map[string]bool{}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("k:%d", i)
		val := "v"
		dbAdd(db, createStringObject(&key, len(key)), createStringObject(&val, len(val)))
		want[key] = true
	}

	// 直接调用 dictScan,模拟 scanGenericCommand 在 count=10000 时的循环
	got := map[string]int{}
	collect := func(privdata interface{}, de *dictEntry) {
		got[(*de.key.ptr).(string)]++
	}
	var cursor uint64
	maxIter := int64(70 * 10)
	for {
		cursor = dictScan(&db.dict, cursor, collect, nil)
		if cursor == 0 {
			break
		}
		maxIter--
		if maxIter <= 0 {
			t.Fatalf("dictScan failed to complete in %d iterations", 70*10)
		}
	}

	missing := []string{}
	for k := range want {
		if got[k] == 0 {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("dictScan missed %d/%d keys, first missing: %v", len(missing), total, missing[:min(5, len(missing))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
