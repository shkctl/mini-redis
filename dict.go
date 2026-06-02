package main

import (
	"math"
)

const dict_hash_function_seed = 5381
const DICT_OK = 0
const DICT_ERR = 1
const DICT_HT_INITIAL_SIZE = 4
const dict_can_resize = 1
const dict_force_resize_ratio = 5

/*
*
字典元素回调处理函数结构定义:
1. privData 数组:索引0为存放元素的链表,索引1 额外的redis obj对象
2. de :当前要处理的键值对
*/
type dictScanFunction func(privData [2]any, de *dictEntry)

/**
 * 字典键值对定义
 */
type dictEntry struct {
	//存储key
	key *robj
	//存储value
	val *robj
	//存储后继节点
	next *dictEntry
}

/**
 * 字典哈希表定义
 */
type dictht struct {
	//存储键值对的数组
	table *[]*dictEntry
	//记录hash table的大小
	size     uint64
	sizemask int
	//记录数组存储了多少个键值对
	used uint64
}

/**
 * 字典核心数据结构定义
 */
type dict struct {
	dType    *dictType
	privdata *interface{}
	//存储键值对的两个数组
	ht        *[2]dictht
	rehashidx int64
	iterators int
}

/**
 * 字典类型特定函数定义
 */
type dictType struct {
	hashFunction  func(key string) int
	keyDup        func(privdata *interface{}, key *interface{}) *interface{}
	valDup        func(privdata *interface{}, obj *interface{}) *interface{}
	keyCompare    func(privdata *interface{}, key1 string, key2 string) bool
	keyDestructor func(privdata *interface{}, key *interface{})
	valDestructor func(privdata *interface{}, obj *interface{})
}

func dictCreate(typePtr *dictType, privDataPtr *interface{}) *dict {
	//初始化字典及其ht数组空间
	d := dict{ht: &[2]dictht{}}
	_dictInit(&d, privDataPtr, typePtr)
	return &d
}

func _dictInit(d *dict, privDataPtr *interface{},
	typePtr *dictType) int {
	//重置哈希表空间
	_dictReset(&(d.ht)[0])
	_dictReset(&(d.ht)[1])

	d.privdata = privDataPtr
	d.dType = typePtr
	//设置rehashidx为-1,代表当前不存在渐进式哈希
	d.rehashidx = -1
	//设置iterators为0,代表字典并不存在迭代
	d.iterators = 0

	return DICT_OK

}

func _dictReset(ht *dictht) {
	ht.table = nil
	ht.size = 0
	ht.sizemask = 0
	ht.used = 0
}

func _dictExpandIfNeeded(d *dict) int {
	if dictIsRehashing(d) {
		return DICT_OK
	}

	if d.ht[0].size == 0 {
		dictExpand(d, DICT_HT_INITIAL_SIZE)
	}

	if d.ht[0].used >= d.ht[0].size &&
		(dict_can_resize == 1 || d.ht[0].used/d.ht[0].size > dict_force_resize_ratio) {
		dictExpand(d, d.ht[0].size<<1)
	}
	return DICT_OK
}

func dictAdd(d *dict, k *robj, v *robj) int {
	//将key存储到哈希表某个索引中,如果成功则返回这个key对应的entry的指针
	entry := dictAddRaw(d, k)
	if entry == nil {
		return DICT_ERR
	}
	//将entry的val设置为v
	entry.val = v
	return DICT_OK
}

func dictAddRaw(d *dict, k *robj) *dictEntry {
	//如果正处于渐进式哈希则会驱逐一部分元素到数组1中
	if dictIsRehashing(d) {
		_dictRehashStep(d)
	}
	//检查索引是否正确，若为-1则说明异常直接返回nil
	index := _dictKeyIndex(d, k)
	//检查key是否存在
	if index == -1 {
		return nil

	}
	//根据渐进式哈希表确定table
	var ht *dictht
	if dictIsRehashing(d) {
		ht = &d.ht[1]
	} else {
		ht = &d.ht[0]
	}

	//通过头插法将元素插入到数组中
	entry := &dictEntry{key: k}
	entry.next = (*(ht.table))[index]
	(*(ht.table))[index] = entry
	//累加used告知数组增加一个元素
	ht.used++

	return entry
}

func dictDelete(ht *dict, key string) int {
	return dictGenericDelete(ht, key, 0)
}

// 删除字典中的key
func dictGenericDelete(d *dict, k string, nofree int) int {
	//若size为0则说明没有bucket还未初始化,直接返回错误
	if d.ht[0].size == 0 {
		return DICT_ERR
	}

	if dictIsRehashing(d) {
		_dictRehashStep(d)
	}
	//哈希定位bucket
	h := dictGenHashFunction(k, len(k))
	var preDe *dictEntry

	for i := 0; i < 2; i++ {
		idx := h & d.ht[i].sizemask
		he := (*(d.ht[i].table))[idx]
		//遍历链表直到找到这个key
		for he != nil {
			if (*he.key.ptr).(string) == k {
				//如果preDe非空则说明被删除元素he在中间,则将he前驱指向he后继
				if preDe != nil {
					preDe.next = he.next
				} else { //否则说明被删除元素he是数组的第一个元素,则直接让数组的第一个元素为he的后继节点
					(*(d.ht[0].table))[idx] = he.next
				}
				//减少used告知数组减少一个元素
				d.ht[i].used--
				if nofree != 0 {
					//help gc
					he = nil
				}
				return DICT_OK
			}
			preDe = he
			he = he.next
		}

		if !dictIsRehashing(d) {
			break
		}
	}

	return DICT_ERR
}

// 原有go map字典操作更新函数,已废弃
func dictReplace_new(d map[string]*robj, key *robj, val *robj) bool {
	k := (*key.ptr).(string)
	if _, e := d[k]; e {
		d[k] = val
		return false
	} else {
		d[k] = val
		return true
	}

}

func dictReplace(d *dict, key *robj, val *robj) bool {
	//先尝试用dictadd添加键值对,若成功则说明这个key不存在,完成后直接返回
	if dictAdd(d, key, val) == DICT_OK {
		return true
	}
	//否则通过dictFind定位到entry,修改值再返回true
	de := dictFind(d, (*key.ptr).(string))
	if de == nil {
		return false
	}
	de.val = val
	return true

}

func dictFind(d *dict, key string) *dictEntry {
	//查看哈希表数组是否都为空,若都为空则直接返回
	if d.ht[0].used+d.ht[1].used == 0 {
		return nil
	}
	//若元素正处于渐进式哈希则进行一次元素驱逐
	if dictIsRehashing(d) {
		_dictRehashStep(d)
	}
	//定位查询key对应的哈希值
	h := dictGenHashFunction(key, len(key))
	//执行最多两次的遍历(因为我们有两个哈希表,一个未扩容前使用,一个出发扩容后作为渐进式哈希的驱逐点)
	for i := 0; i < 2; i++ {
		//基于位运算定位索引
		idx := h & d.ht[i].sizemask
		//定位到对应bucket桶,通过遍历定位到本次要检索的key
		he := (*d.ht[0].table)[idx]
		for he != nil {
			if (*he.key.ptr).(string) == key {
				return he
			}
			he = he.next
		}
		//若未进行渐进式哈希则说明哈希表-1没有元素,直接结束循环,反之执行2次遍历
		if !dictIsRehashing(d) {
			break
		}
	}

	return nil

}

func dictIsRehashing(d *dict) bool {
	return d.rehashidx != -1
}

func _dictKeyIndex(d *dict, key *robj) int {

	var idx int
	if _dictExpandIfNeeded(d) == DICT_ERR {
		return -1
	}

	//计算索引位置
	h := d.dType.hashFunction((*key.ptr).(string))

	//基于索引定位key
	for i := 0; i < 2; i++ {
		//通过位运算计算数组存储的索引
		idx = h & d.ht[i].sizemask
		he := (*(d.ht[i].table))[idx]
		//判断这个索引下是否存在相同的key,如果存在则返回-1,告知外部不用添加entry,有需要直接改dictentry的val即可
		for he != nil {
			if d.dType.keyCompare(nil, (*key.ptr).(string), (*he.key.ptr).(string)) {
				return -1
			}
			he = he.next
		}

		// 如果正在rehash，则检查ht[1]
		if !dictIsRehashing(d) {
			break
		}
	}

	return idx
}

func _dictRehashStep(d *dict) {
	if d.iterators == 0 {
		dictRehash(d, 1)
	}
}

// 渐进式哈希
func dictRehash(d *dict, n int) int {
	//最大容错次数
	empty_visits := n * 10

	if !dictIsRehashing(d) {
		return 0
	}
	//循环n次的渐进式重试，在最大限制内完成
	for n > 0 && d.ht[0].used != 0 {
		n--
		var de *dictEntry
		var nextde *dictEntry
		//定位到非空的bucket桶
		for (*(d.ht[0].table))[d.rehashidx] == nil {
			d.rehashidx++
			empty_visits--
			//一旦访问空bucket超过10次则返回
			if empty_visits == 0 {
				return 1
			}
		}
		//从非空的bucket桶开始
		de = (*(d.ht[0].table))[d.rehashidx]

		for de != nil {
			//再哈希定位元素通过头插法迁移元素到ht[1]
			nextde = de.next
			h := dictGenHashFunction((*de.key.ptr).(string), len((*de.key.ptr).(string))) & d.ht[1].sizemask
			de.next = (*(d.ht[1].table))[h]
			(*(d.ht[1].table))[h] = de

			d.ht[0].used--
			d.ht[1].used++

			de = nextde
		}
		//rehashidx+1告知下一次驱逐的索引位置
		(*(d.ht[0].table))[d.rehashidx] = nil
		d.rehashidx++
	}
	//ht[0]为空则原子交换,将ht[1]变为ht[0]
	if d.ht[0].used == 0 {
		d.ht[0].table = nil
		d.ht[0] = d.ht[1]
		_dictReset(&(d.ht[1]))
		d.rehashidx = -1
	}

	return 1

}

// 字典扩容
func dictExpand(d *dict, size uint64) int {
	var n dictht
	//获取实际空间
	realSize := _dictNextPower(size)
	//健壮性校验
	if dictIsRehashing(d) || d.ht[0].used > size {
		return DICT_ERR
	}

	if realSize == d.ht[0].size {
		return DICT_ERR
	}

	n.size = realSize
	n.sizemask = int(realSize - 1)
	n.table = &[]*dictEntry{}
	table := make([]*dictEntry, realSize)
	n.table = &table

	n.used = 0

	if d.ht[0].table == nil {
		d.ht[0] = n
		return DICT_OK
	}
	d.ht[1] = n
	d.rehashidx = 0

	return DICT_OK

}

func _dictNextPower(size uint64) uint64 {
	i := DICT_HT_INITIAL_SIZE

	if size >= math.MaxInt64 {
		return math.MaxInt64 + 1
	}

	for true {
		if uint64(i) >= size {
			break
		}
		i = i << 1
	}
	return uint64(i)
}

func dictGetKey(he *dictEntry) *robj {
	return he.key
}

func dictGetVal(he *dictEntry) *robj {
	return he.val
}

func dictSize(d *dict) uint64 {
	return d.ht[0].used + d.ht[1].used
}

// 反转二进制位
func rev(v uint64) uint64 {
	s := uint64(64) // 8 * sizeof(uint64)
	mask := ^uint64(0)
	s >>= 1
	for s > 0 {
		mask ^= (mask << s)
		v = ((v >> s) & mask) | ((v << s) & ^mask)
		s >>= 1
	}
	return v
}

func dictScan(d *dict,
	v int64,
	fn dictScanFunction,
	privData [2]any) int64 {

	var t0, t1 *dictht
	var de *dictEntry
	var m0, m1 int64

	if dictSize(d) == 0 {
		return 0
	}

	if !dictIsRehashing(d) {
		t0 = &(d.ht[0])
		m0 = int64(t0.sizemask)

		de = (*t0.table)[v&m0]

		for de != nil {
			fn(privData, de)
			de = de.next
		}

		v |= ^m0

		v = int64(rev(uint64(v)))
		v++
		v = int64(rev(uint64(v)))
	} else {
		t0 = &(d.ht[0])
		t1 = &(d.ht[1])

		if t0.used > t1.used {
			t1 = &(d.ht[0])
			t0 = &(d.ht[1])
		}

		m0 = int64(t0.sizemask)
		m1 = int64(t1.sizemask)

		de = (*t0.table)[v&m0]

		for de != nil {
			fn(privData, de)
			de = de.next
		}

		for {
			de = (*t0.table)[v&m1]

			for de != nil {
				fn(privData, de)
				de = de.next
			}

			v |= ^m1

			v |= ^m1

			v = int64(rev(uint64(v)))
			v++
			v = int64(rev(uint64(v)))

			if v&(m0^m1) == 0 {
				break
			}

		}

	}

	return v
}
