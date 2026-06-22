package main

import (
	"encoding/binary"
	"math/rand"
	"testing"
)

/* intset 位宽方案三方对照压测。
 *
 * 目的:验证「统一 []int64(方案 A)」是否真的不如 Redis 的「按需位宽」。
 * 不动生产代码 intset.go(方案 A),把 B/C 两种候选实现自包含在本测试文件里:
 *
 *   A —— 现状:contents []int64 统一存储(intset.go)
 *   B —— 忠实复刻 Redis:contents []byte,按 encoding 紧凑读写(2/4/8 字节,小端)
 *   C —— Go 折中:[]int16 / []int32 / []int64 三个具体类型切片,按需升级
 *
 * 对比维度:
 *   Build  —— 批量插入(含二分 + 移位 + 扩容),配合 -benchmem 同时反映内存占用
 *   Search —— 纯读路径(二分查找)
 *   Churn  —— Remove + Add 往返(双向移位)
 *
 * 数据分布:
 *   int16 —— 全部落在 int16 范围:A 每元素 8B,B/C 每元素 2B(内存差异最大场景)
 *   int64 —— 全部超出 int32:三者都用 8B(检验 B 的编解码开销在「无内存收益」时是否拖后腿)
 *
 * 运行:go test -run=^$ -bench=BenchmarkIntset -benchmem
 */

// 防止编译器消除被测对象 / 结果
var sinkInt int
var sinkBool bool

// ---------------------------------------------------------------------------
// 方案 B:忠实复刻 Redis —— contents []byte,按 encoding 紧凑存储
// ---------------------------------------------------------------------------

type bintset struct {
	encoding int
	length   int
	contents []byte // 每元素占 encoding 字节,小端序
}

func newBintset() *bintset { return &bintset{encoding: INTSET_ENC_INT16} }

func bintsetGetEnc(is *bintset, pos, enc int) int64 {
	off := pos * enc
	switch enc {
	case INTSET_ENC_INT64:
		return int64(binary.LittleEndian.Uint64(is.contents[off:]))
	case INTSET_ENC_INT32:
		return int64(int32(binary.LittleEndian.Uint32(is.contents[off:])))
	default:
		return int64(int16(binary.LittleEndian.Uint16(is.contents[off:])))
	}
}

func bintsetGet(is *bintset, pos int) int64 { return bintsetGetEnc(is, pos, is.encoding) }

func bintsetSet(is *bintset, pos int, val int64) {
	off := pos * is.encoding
	switch is.encoding {
	case INTSET_ENC_INT64:
		binary.LittleEndian.PutUint64(is.contents[off:], uint64(val))
	case INTSET_ENC_INT32:
		binary.LittleEndian.PutUint32(is.contents[off:], uint32(val))
	default:
		binary.LittleEndian.PutUint16(is.contents[off:], uint16(val))
	}
}

// bintsetResize 调整 contents 到 length 个元素。
//
// 注意:真实 Redis intset 每次 add 都 zrealloc 到精确大小(O(n) 拷贝,极致紧凑,
// 不摊还),靠 jemalloc 的 size-class 复用兜底。但本压测要隔离的唯一变量是
// 「元素位宽」,故这里刻意采用与 Go append / 方案 A / 方案 C 一致的「容量倍增」
// 摊还策略 —— 否则 B 的扩容次数远多于 A/C,Build 的内存/allocs 对照会被
// 「扩容策略差异」污染,而非反映位宽本身。
func bintsetResize(is *bintset, length int) {
	need := length * is.encoding
	if need <= cap(is.contents) {
		is.contents = is.contents[:need]
		return
	}
	newcap := cap(is.contents) * 2
	if newcap < need {
		newcap = need
	}
	nc := make([]byte, need, newcap)
	copy(nc, is.contents)
	is.contents = nc
}

// copy 具备 memmove 语义,可安全处理重叠搬移
func bintsetMoveTail(is *bintset, from, to int) {
	enc := is.encoding
	n := (is.length - from) * enc
	copy(is.contents[to*enc:to*enc+n], is.contents[from*enc:from*enc+n])
}

func bintsetSearch(is *bintset, val int64) (bool, int) {
	if is.length == 0 {
		return false, 0
	}
	if val > bintsetGet(is, is.length-1) {
		return false, is.length
	}
	if val < bintsetGet(is, 0) {
		return false, 0
	}
	lo, hi := 0, is.length-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		cur := bintsetGet(is, mid)
		if cur == val {
			return true, mid
		} else if cur > val {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return false, lo
}

func bintsetUpgradeAndAdd(is *bintset, val int64) int {
	curenc := is.encoding
	newenc := intsetValueEncoding(val)
	length := is.length
	prepend := 0 // val 超宽必在两端:负数排最前,正数排最后
	if val < 0 {
		prepend = 1
	}
	is.encoding = newenc
	bintsetResize(is, is.length+1)
	// 从后往前按新位宽重排旧元素,避免覆盖未读数据
	for i := length - 1; i >= 0; i-- {
		bintsetSet(is, i+prepend, bintsetGetEnc(is, i, curenc))
	}
	if prepend == 1 {
		bintsetSet(is, 0, val)
	} else {
		bintsetSet(is, is.length, val)
	}
	is.length++
	return 1
}

func bintsetAdd(is *bintset, val int64) int {
	if intsetValueEncoding(val) > is.encoding {
		return bintsetUpgradeAndAdd(is, val)
	}
	found, pos := bintsetSearch(is, val)
	if found {
		return 0
	}
	bintsetResize(is, is.length+1)
	if pos < is.length {
		bintsetMoveTail(is, pos, pos+1)
	}
	bintsetSet(is, pos, val)
	is.length++
	return 1
}

func bintsetRemove(is *bintset, val int64) int {
	if intsetValueEncoding(val) > is.encoding {
		return 0
	}
	found, pos := bintsetSearch(is, val)
	if !found {
		return 0
	}
	if pos < is.length-1 {
		bintsetMoveTail(is, pos+1, pos)
	}
	bintsetResize(is, is.length-1)
	is.length--
	return 1
}

// ---------------------------------------------------------------------------
// 方案 C:Go 折中 —— 三个具体类型切片,保留类型安全,免编解码
// ---------------------------------------------------------------------------

type tintset struct {
	encoding int
	length   int
	c16      []int16
	c32      []int32
	c64      []int64
}

func newTintset() *tintset { return &tintset{encoding: INTSET_ENC_INT16} }

func tintsetGet(is *tintset, pos int) int64 {
	switch is.encoding {
	case INTSET_ENC_INT64:
		return is.c64[pos]
	case INTSET_ENC_INT32:
		return int64(is.c32[pos])
	default:
		return int64(is.c16[pos])
	}
}

func tintsetSearch(is *tintset, val int64) (bool, int) {
	if is.length == 0 {
		return false, 0
	}
	if val > tintsetGet(is, is.length-1) {
		return false, is.length
	}
	if val < tintsetGet(is, 0) {
		return false, 0
	}
	lo, hi := 0, is.length-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		cur := tintsetGet(is, mid)
		if cur == val {
			return true, mid
		} else if cur > val {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return false, lo
}

func tintsetUpgrade(is *tintset, newenc int) {
	switch newenc {
	case INTSET_ENC_INT64:
		nc := make([]int64, is.length, is.length+1)
		for i := 0; i < is.length; i++ {
			nc[i] = tintsetGet(is, i)
		}
		is.c64, is.c32, is.c16 = nc, nil, nil
	case INTSET_ENC_INT32:
		nc := make([]int32, is.length, is.length+1)
		for i := 0; i < is.length; i++ {
			nc[i] = int32(tintsetGet(is, i))
		}
		is.c32, is.c16 = nc, nil
	}
	is.encoding = newenc
}

func tintsetAdd(is *tintset, val int64) int {
	if newenc := intsetValueEncoding(val); newenc > is.encoding {
		tintsetUpgrade(is, newenc)
	}
	found, pos := tintsetSearch(is, val)
	if found {
		return 0
	}
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

func tintsetRemove(is *tintset, val int64) int {
	if intsetValueEncoding(val) > is.encoding {
		return 0
	}
	found, pos := tintsetSearch(is, val)
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

// ---------------------------------------------------------------------------
// 测试数据生成
// ---------------------------------------------------------------------------

// genVals 生成 n 个去重的乱序整数。enc=2 → 全 int16 范围;enc=8 → 全超 int32。
func genVals(n, enc int) []int64 {
	vals := make([]int64, n)
	for i := 0; i < n; i++ {
		if enc == INTSET_ENC_INT16 {
			vals[i] = int64(i - n/2) // 落在 int16 范围(n 远小于 65536)
		} else {
			vals[i] = int64(i)*1_000_000_000 + 5_000_000_000 // > int32
		}
	}
	r := rand.New(rand.NewSource(1)) // 固定种子,三方用同一插入顺序
	r.Shuffle(n, func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
	return vals
}

type dist struct {
	name string
	enc  int
}

// 生产 intset 上限 512;额外加 4096 观察规模放大后的 cache 效应
var benchSizes = []int{512, 4096}
var benchDists = []dist{{"int16", INTSET_ENC_INT16}, {"int64", INTSET_ENC_INT64}}

// ---------------------------------------------------------------------------
// 正确性交叉校验:压测的前提是三方语义完全一致。
// 用同一组乱序(含负数,触发 prepend 升级路径)插入 A/B/C,
// 断言三者最终都是「同一升序序列」,否则性能数字没有可比意义。
// ---------------------------------------------------------------------------

func TestIntsetVariantsEquivalent(t *testing.T) {
	for _, d := range benchDists {
		for _, sz := range []int{1, 2, 512, 4096} {
			vals := genVals(sz, d.enc)
			// 追加几个负值,强制走 bintsetUpgradeAndAdd 的 prepend 分支
			vals = append(vals, -1, -100000, -5_000_000_000)

			isA := &intset{encoding: INTSET_ENC_INT16}
			isB := newBintset()
			isC := newTintset()
			for _, v := range vals {
				intsetAdd(isA, v)
				bintsetAdd(isB, v)
				tintsetAdd(isC, v)
			}

			if isA.length != isB.length || isA.length != isC.length {
				t.Fatalf("%s/%d: length 不一致 A=%d B=%d C=%d", d.name, sz, isA.length, isB.length, isC.length)
			}
			for i := 0; i < isA.length; i++ {
				a, b, c := intsetGet(isA, i), bintsetGet(isB, i), tintsetGet(isC, i)
				if a != b || a != c {
					t.Fatalf("%s/%d: 第 %d 个元素不一致 A=%d B=%d C=%d", d.name, sz, i, a, b, c)
				}
				if i > 0 && a <= intsetGet(isA, i-1) {
					t.Fatalf("%s/%d: A 在 %d 处升序被破坏", d.name, sz, i)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Build:批量插入(速度) + -benchmem(内存)
// ---------------------------------------------------------------------------

func BenchmarkIntsetBuild(b *testing.B) {
	for _, sz := range benchSizes {
		for _, d := range benchDists {
			vals := genVals(sz, d.enc)

			b.Run("A/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					is := &intset{encoding: INTSET_ENC_INT16}
					for _, v := range vals {
						intsetAdd(is, v)
					}
					sinkInt = is.length
				}
			})
			b.Run("B/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					is := newBintset()
					for _, v := range vals {
						bintsetAdd(is, v)
					}
					sinkInt = is.length
				}
			})
			b.Run("C/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					is := newTintset()
					for _, v := range vals {
						tintsetAdd(is, v)
					}
					sinkInt = is.length
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Search:纯二分查找读路径
// ---------------------------------------------------------------------------

func BenchmarkIntsetSearch(b *testing.B) {
	for _, sz := range benchSizes {
		for _, d := range benchDists {
			vals := genVals(sz, d.enc)
			n := len(vals)

			isA := &intset{encoding: INTSET_ENC_INT16}
			isB := newBintset()
			isC := newTintset()
			for _, v := range vals {
				intsetAdd(isA, v)
				bintsetAdd(isB, v)
				tintsetAdd(isC, v)
			}

			b.Run("A/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				var f bool
				for i := 0; i < b.N; i++ {
					f, _ = intsetSearch(isA, vals[i%n])
				}
				sinkBool = f
			})
			b.Run("B/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				var f bool
				for i := 0; i < b.N; i++ {
					f, _ = bintsetSearch(isB, vals[i%n])
				}
				sinkBool = f
			})
			b.Run("C/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				var f bool
				for i := 0; i < b.N; i++ {
					f, _ = tintsetSearch(isC, vals[i%n])
				}
				sinkBool = f
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Churn:Remove + Add 往返(双向移位)
// ---------------------------------------------------------------------------

func BenchmarkIntsetChurn(b *testing.B) {
	for _, sz := range benchSizes {
		for _, d := range benchDists {
			vals := genVals(sz, d.enc)
			n := len(vals)

			isA := &intset{encoding: INTSET_ENC_INT16}
			isB := newBintset()
			isC := newTintset()
			for _, v := range vals {
				intsetAdd(isA, v)
				bintsetAdd(isB, v)
				tintsetAdd(isC, v)
			}

			b.Run("A/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					v := vals[i%n]
					intsetRemove(isA, v)
					intsetAdd(isA, v)
				}
			})
			b.Run("B/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					v := vals[i%n]
					bintsetRemove(isB, v)
					bintsetAdd(isB, v)
				}
			})
			b.Run("C/"+d.name+"/"+itoa(sz), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					v := vals[i%n]
					tintsetRemove(isC, v)
					tintsetAdd(isC, v)
				}
			})
		}
	}
}

// itoa 避免引入 strconv 只为拼基准名(与生产代码风格一致,小工具内联)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
