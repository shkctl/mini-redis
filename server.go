package main

import ()

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
	const m uint32 = 0x5bd1e995
	const r uint32 = 24

	// MurmurHash2：按字节读取，每轮处理 4 个字节
	data := key
	h := uint32(dict_hash_function_seed) ^ uint32(kLen)

	pos := 0
	for kLen >= 4 {
		k := uint32(data[pos]) | uint32(data[pos+1])<<8 |
			uint32(data[pos+2])<<16 | uint32(data[pos+3])<<24
		k *= m
		k ^= k >> r
		k *= m
		h *= m
		h ^= k

		pos += 4
		kLen -= 4
	}

	// 处理尾部不足 4 字节的部分
	switch kLen {
	case 3:
		h ^= uint32(data[pos+2]) << 16
		fallthrough
	case 2:
		h ^= uint32(data[pos+1]) << 8
		fallthrough
	case 1:
		h ^= uint32(data[pos])
		h *= m
	}

	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return int(h)

}

func dictCompare(privdata *interface{}, key1 string, key2 string) bool {
	return key1 == key2
}
