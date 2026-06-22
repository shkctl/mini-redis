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

	// 取同一桶里的若干 key 触发扩容/渐进式 rehash，并记录实际添加的 key
	var keys []string
	for _, v := range (*bucketMap)[0] {
		dictAdd(d, createStringObject(&v, len(v)), nil)
		keys = append(keys, v)
		if len(keys) == 4 {
			break
		}
	}

	k := "1000"
	dictAdd(d, createStringObject(&k, len(k)), nil)
	keys = append(keys, k)

	// 此时应处于渐进式 rehash 中，验证所有 key 仍可在两张表间查到
	if !dictIsRehashing(d) {
		t.Fatalf("expected dict to be rehashing, rehashidx=%d", d.rehashidx)
	}
	for _, key := range keys {
		if dictFind(d, key) == nil {
			t.Fatalf("rehash fail: key %q not found", key)
		}
	}
}
