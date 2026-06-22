package main

import (
	"math"
	"math/rand"
)

/* intset 位宽常量 —— 对应 Redis intset.c 的 INTSET_ENC_INT16/32/64。
 * 在 C 里这些值代表每个元素占用的字节数,contents 用柔性数组按位宽读取。
 *
 * 本 Go 实现采用「方案 C」:用三个具体类型切片(c16/c32/c64)按需升级存储,
 * encoding 标记当前生效的那一个。相比统一 []int64,小整数集合每元素从 8B
 * 压到 2B(省 4×),达到 Redis「按需位宽、提升局部性」的目的;相比忠实复刻
 * 的 []byte 紧凑存储,又免去每次访问的小端编解码开销,且保留 Go 类型安全。
 * (三方压测见 intset_bench_test.go:C 内存≈[]byte,速度≈[]int64 或更快。)
 */
const (
	INTSET_ENC_INT16 = 2
	INTSET_ENC_INT32 = 4
	INTSET_ENC_INT64 = 8
)

/* set-max-intset-entries: 集合元素数超过此值时,
 * setTypeAdd 会把 intset 整体升级成 HT 编码。
 */
const REDIS_SET_MAX_INTSET_ENTRIES = 512

/* intset 数据结构(方案 C)。
 * 任一时刻只有 encoding 对应的那个切片生效(其余为 nil),始终保持升序 ——
 * 这是 intset 能用二分查找 O(log n) 的前提。
 */
type intset struct {
	encoding int     // 当前生效位宽(INTSET_ENC_INT16/32/64)
	length   int     // 元素个数
	c16      []int16 // encoding==INT16 时生效
	c32      []int32 // encoding==INT32 时生效
	c64      []int64 // encoding==INT64 时生效
}

/* intsetValueEncoding 按值大小返回所需位宽,对应 Redis intsetValueEncoding。
 * 超出 int32 范围 → INT64;超出 int16 范围 → INT32;否则 INT16。
 */
func intsetValueEncoding(v int64) int {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return INTSET_ENC_INT64
	} else if v < math.MinInt16 || v > math.MaxInt16 {
		return INTSET_ENC_INT32
	}
	return INTSET_ENC_INT16
}

/* createIntsetObject 创建一个 intset 编码的 SET 对象。
 * 初始为 INT16 编码、空集合;三个切片均为 nil,首次 append 时按需分配。
 */
func createIntsetObject() *robj {
	is := &intset{encoding: INTSET_ENC_INT16}
	i := interface{}(is)
	o := createObject(REDIS_SET, &i)
	o.encoding = REDIS_ENCODING_INTSET
	return o
}

/* intsetGet 读取 pos 处元素,按当前 encoding 从对应切片取值并统一返回 int64。
 * intset 对外只暴露 int64 语义,底层用哪种位宽存储对调用方透明。
 */
func intsetGet(is *intset, pos int) int64 {
	switch is.encoding {
	case INTSET_ENC_INT64:
		return is.c64[pos]
	case INTSET_ENC_INT32:
		return int64(is.c32[pos])
	default:
		return int64(is.c16[pos])
	}
}

/* intsetSearch 在升序数组中二分查找 val。
 * 返回 found=true  时 pos 为命中元素的下标;
 * 返回 found=false 时 pos 为应插入位置(保持升序)。
 *
 * intset 的增删查全部建立在此函数之上,它是 intset 的算法核心。
 */
func intsetSearch(is *intset, val int64) (found bool, pos int) {
	// 空集合:直接返回插入位置 0
	if is.length == 0 {
		return false, 0
	}
	// val 大于当前最大值,必为新元素,追加到末尾
	if val > intsetGet(is, is.length-1) {
		return false, is.length
	}
	// val 小于当前最小值,必为新元素,插到最前
	if val < intsetGet(is, 0) {
		return false, 0
	}
	// 在闭区间 [0, is.length-1] 上二分查找。
	// 循环条件必须是 <= 而非 <:lo==hi 时仍需检查最后一个元素,
	// 否则会漏掉"恰好落在末位"的命中,导致集合去重失效。
	lo := 0
	hi := is.length - 1
	for lo <= hi {
		mid := lo + (hi-lo)/2 // 防溢出写法,等价于 (lo+hi)/2
		cur := intsetGet(is, mid)
		if cur == val {
			return true, mid
		} else if cur > val {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}

	// 未命中:循环退出时 lo 恰好落在"第一个大于 val 的位置"。
	// 注意返回 lo 而非 mid —— mid 是上一轮的旧值,已失效。
	return false, lo
}

/* intsetUpgrade 把底层存储升级到更宽的 newenc。
 * 逐个把现有元素从旧切片搬到新切片(预留 +1 容量,紧接着的插入免再次扩容),
 * 旧切片置 nil 交给 GC。升级后再走正常的二分插入,无需 Redis 那种
 * 「prepend/append 端点捷径」—— Go 切片插入已足够简洁,正确性优先。
 */
func intsetUpgrade(is *intset, newenc int) {
	switch newenc {
	case INTSET_ENC_INT64:
		nc := make([]int64, is.length, is.length+1)
		for i := 0; i < is.length; i++ {
			nc[i] = intsetGet(is, i)
		}
		is.c64, is.c32, is.c16 = nc, nil, nil
	case INTSET_ENC_INT32:
		nc := make([]int32, is.length, is.length+1)
		for i := 0; i < is.length; i++ {
			nc[i] = int32(intsetGet(is, i))
		}
		is.c32, is.c16 = nc, nil
	}
	is.encoding = newenc
}

/* intsetAdd 向 intset 添加 val。
 * 返回 1=新增成功,0=已存在(幂等,集合语义)。
 */
func intsetAdd(is *intset, val int64) int {
	// 新值需要更宽编码时,先整体升级位宽,再按统一编码插入
	if newenc := intsetValueEncoding(val); newenc > is.encoding {
		intsetUpgrade(is, newenc)
	}
	found, pos := intsetSearch(is, val)
	if found {
		return 0
	}
	// 在 pos 处插入并保持升序:先扩容一位,再把 [pos:] 后移
	switch is.encoding {
	case INTSET_ENC_INT64:
		is.c64 = append(is.c64, 0)
		copy(is.c64[pos+1:], is.c64[pos:])
		is.c64[pos] = val
	case INTSET_ENC_INT32:
		is.c32 = append(is.c32, 0)
		copy(is.c32[pos+1:], is.c32[pos:])
		is.c32[pos] = int32(val)
	default:
		is.c16 = append(is.c16, 0)
		copy(is.c16[pos+1:], is.c16[pos:])
		is.c16[pos] = int16(val)
	}
	is.length++
	return 1
}

/* intsetRemove 从 intset 删除 val。
 * 返回 1=删除成功,0=不存在。
 */
func intsetRemove(is *intset, val int64) int {
	// val 比当前编码还宽,绝不可能存在,直接短路
	if intsetValueEncoding(val) > is.encoding {
		return 0
	}
	found, pos := intsetSearch(is, val)
	if !found {
		return 0
	}
	switch is.encoding {
	case INTSET_ENC_INT64:
		is.c64 = append(is.c64[:pos], is.c64[pos+1:]...)
	case INTSET_ENC_INT32:
		is.c32 = append(is.c32[:pos], is.c32[pos+1:]...)
	default:
		is.c16 = append(is.c16[:pos], is.c16[pos+1:]...)
	}
	is.length--
	return 1
}

/* intsetRandom 随机返回一个元素(SPOP 使用)。
 * 空集合返回 (_, false)。
 */
func intsetRandom(is *intset) (int64, bool) {
	if is.length == 0 {
		return 0, false
	}
	return intsetGet(is, rand.Intn(is.length)), true
}
