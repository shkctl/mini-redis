package main

import (
	"unsafe"
)

/*Error codes */

const C_OK = 0
const C_ERR = -1

var dbDictType = dictType{
	hashFunction:  dictSdsHash,
	keyDup:        nil,
	valDup:        nil,
	keyCompare:    dictCompare,
	keyDestructor: nil,
	valDestructor: nil,
}

func dictSdsHash(key string) int {
	return dictGenHashFunction(key, len(key))
}

func dictGenHashFunction(key string, kLen int) int {
	var seed int = dict_hash_function_seed
	var m int = 0x5bd1e995
	var r int = 24

	var h int = seed ^ kLen

	runes := []rune(key)
	pos := 0

	for kLen >= 4 {
		k := *(*int)(unsafe.Pointer(&runes[pos]))
		k *= m
		k ^= k >> r
		k *= m
		h ^= k

		pos += 4
		kLen -= 4
	}

	switch kLen {
	case 3:
		h ^= int(runes[2]) << 16

	case 2:
		h ^= int(runes[1]) << 8

	case 1:
		h ^= int(runes[0])
		h *= m
	}

	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h

}

func dictCompare(privdata *interface{}, key1 string, key2 string) bool {
	return key1 == key2
}
