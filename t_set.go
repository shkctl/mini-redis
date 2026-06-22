package main

import (
	"strconv"
)

/* ============================ setType 抽象层 ============================
 * 命令处理函数只调用本层 API,永远不直接触碰 intset 或底层 map。
 * HT 编码用 Go map[string]struct{} —— 集合元素无值,struct{} 零开销;
 * 且 map 的桶式布局 cache 局部性好,贴合 Redis 读多/扫描多的负载。
 * 这是 Set 类型"双编码"对上层透明的关键(策略模式)。
 */

/* createSetObject 创建一个 HT 编码(哈希表)的空集合对象 */
func createSetObject() *robj {
	m := map[string]struct{}{}
	i := interface{}(m)
	o := createObject(REDIS_SET, &i)
	o.encoding = REDIS_ENCODING_HT
	return o
}

/* setObjectGetIntegerValue 尝试从成员 robj 中取出 int64。
 * 成员可能是 EMBSTR 字符串(ptr 为 string)或 INT 编码整数(ptr 为 int64)。
 * 成功返回 (值, true);否则 (0, false)。
 */
func setObjectGetIntegerValue(o *robj) (int64, bool) {
	switch v := (*o.ptr).(type) {
	case int64:
		return v, true
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

/* setMemberKey 取成员对应的字符串键(HT 编码下用作 map key)。
 * INT 编码成员统一转十进制串,保证 "5"(串) 与 5(整) 成员判定一致,
 * 与 Redis 用 dictEncObjKeyCompare 做跨编码比较的语义吻合。
 */
func setMemberKey(value *robj) string {
	switch v := (*value.ptr).(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}

/* setTypeCreate 按首个成员类型选择初始编码:
 * 整数 → intset(紧凑);非整数 → HT。
 * 与 Redis setTypeCreate 一致,避免"先建 intset 又立刻升级"的无谓开销。
 */
func setTypeCreate(value *robj) *robj {
	if _, ok := setObjectGetIntegerValue(value); ok {
		return createIntsetObject()
	}
	return createSetObject()
}

/* setTypeAdd 向集合添加成员,返回 1=新增 0=已存在。
 * 内部处理编码升级:intset 下若成员非整数、或元素数超阈值,自动转 HT。
 */
func setTypeAdd(subject *robj, value *robj) int {
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		key := setMemberKey(value)
		if _, exists := m[key]; exists {
			return 0
		}
		m[key] = struct{}{}
		return 1
	}

	/* intset 编码 */
	if llval, ok := setObjectGetIntegerValue(value); ok {
		is := (*subject.ptr).(*intset)
		added := intsetAdd(is, llval)
		// 元素数超过阈值 → 升级成 HT
		if added == 1 && is.length > REDIS_SET_MAX_INTSET_ENTRIES {
			setTypeConvert(subject)
		}
		return added
	}

	/* 成员非整数,intset 容不下 → 先整体转 HT,再走 HT 分支插入 */
	setTypeConvert(subject)
	return setTypeAdd(subject, value)
}

/* setTypeRemove 从集合删除成员,返回 1=删除 0=不存在 */
func setTypeRemove(subject *robj, value *robj) int {
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		key := setMemberKey(value)
		if _, exists := m[key]; !exists {
			return 0
		}
		delete(m, key)
		return 1
	}
	/* intset:非整数成员不可能存在 */
	if llval, ok := setObjectGetIntegerValue(value); ok {
		is := (*subject.ptr).(*intset)
		return intsetRemove(is, llval)
	}
	return 0
}

/* setTypeIsMember 判断成员是否存在 */
func setTypeIsMember(subject *robj, value *robj) bool {
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		_, exists := m[setMemberKey(value)]
		return exists
	}
	if llval, ok := setObjectGetIntegerValue(value); ok {
		is := (*subject.ptr).(*intset)
		found, _ := intsetSearch(is, llval)
		return found
	}
	return false
}

/* setTypeSize 返回元素个数 */
func setTypeSize(subject *robj) int64 {
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		return int64(len(m))
	}
	is := (*subject.ptr).(*intset)
	return int64(is.length)
}

/* setTypeRandomElement 随机返回一个成员(SPOP 用)。
 * HT 下复用 Go map 迭代的随机起始序(贴近 Redis 随机语义)。
 */
func setTypeRandomElement(subject *robj) (*robj, bool) {
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		for k := range m {
			s := k
			return createStringObject(&s, len(s)), true
		}
		return nil, false
	}
	is := (*subject.ptr).(*intset)
	if v, ok := intsetRandom(is); ok {
		s := strconv.FormatInt(v, 10)
		return createStringObject(&s, len(s)), true
	}
	return nil, false
}

/* setTypeConvert 将集合从 intset 整体升级为 HT(map)编码(单向)。 */
func setTypeConvert(subject *robj) {
	if subject.encoding != REDIS_ENCODING_INTSET {
		return
	}
	is := (*subject.ptr).(*intset)
	m := make(map[string]struct{}, is.length)
	for i := 0; i < is.length; i++ {
		m[strconv.FormatInt(intsetGet(is, i), 10)] = struct{}{}
	}
	newPtr := interface{}(m)
	subject.ptr = &newPtr
	subject.encoding = REDIS_ENCODING_HT
}

/* --- 迭代器(SMEMBERS / 集合运算用)--- */

type setTypeIterator struct {
	subject *robj
	htKeys  []string // HT 模式:预先快照的 key 列表
	htIdx   int
	isIdx   int // intset 模式:数组游标
}

func setTypeInitIterator(subject *robj) *setTypeIterator {
	it := &setTypeIterator{subject: subject}
	if subject.encoding == REDIS_ENCODING_HT {
		m := (*subject.ptr).(map[string]struct{})
		it.htKeys = make([]string, 0, len(m))
		for k := range m {
			it.htKeys = append(it.htKeys, k)
		}
	}
	return it
}

func setTypeReleaseIterator(it *setTypeIterator) {}

func setTypeNext(it *setTypeIterator) (*robj, bool) {
	if it.subject.encoding == REDIS_ENCODING_HT {
		if it.htIdx >= len(it.htKeys) {
			return nil, false
		}
		s := it.htKeys[it.htIdx]
		it.htIdx++
		return createStringObject(&s, len(s)), true
	}
	is := (*it.subject.ptr).(*intset)
	if it.isIdx >= is.length {
		return nil, false
	}
	v := intsetGet(is, it.isIdx)
	it.isIdx++
	s := strconv.FormatInt(v, 10)
	return createStringObject(&s, len(s)), true
}

/* setTypeLookupWriteOrCreate 查找集合,不存在则创建空 HT 集合。
 * 类型不符回复 WRONGTYPE。供需要"写时创建"语义的命令使用。
 */
func setTypeLookupWriteOrCreate(c *redisClient, key *robj) *robj {
	o := lookupKeyWrite(c.db, key)
	if o == nil {
		o = createSetObject()
		dbAdd(c.db, key, o)
		return o
	}
	if o.robjType != REDIS_SET {
		addReply(c, shared.wrongtypeerr)
		return nil
	}
	return o
}

/* ============================== 基础命令 ============================== */

/* saddCommand: SADD key member [member ...]
 * 返回实际新增的成员个数(已存在的不计)。
 */
func saddCommand(c *redisClient) {
	set := lookupKeyWrite(c.db, c.argv[1])
	// 仅当 key 存在且类型不符时才报错;nil 表示 key 不存在(稍后创建)
	// 注意:本项目的 checkType 非 nil 安全,必须先判 set != nil 再调用
	if set != nil && checkType(c, set, REDIS_SET) {
		return
	}
	// 不存在则按首个成员类型选择初始编码
	if set == nil {
		set = setTypeCreate(c.argv[2])
		dbAdd(c.db, c.argv[1], set)
	}

	var added int64
	var i uint64
	for i = 2; i < c.argc; i++ {
		added += int64(setTypeAdd(set, c.argv[i]))
	}
	addReplyLongLong(c, added)
}

/* sremCommand: SREM key member [member ...]
 * 返回实际删除的成员个数;删空后移除 key。
 */
func sremCommand(c *redisClient) {
	o := lookupKeyWriteOrReply(c, c.argv[1], shared.czero)
	if o == nil || checkType(c, o, REDIS_SET) {
		return
	}

	var deleted int64
	var i uint64
	for i = 2; i < c.argc; i++ {
		deleted += int64(setTypeRemove(o, c.argv[i]))
	}
	// 删空后直接移除 key(与 Redis / 项目 hdel 行为一致)
	if setTypeSize(o) == 0 {
		dbDelete(c.db, c.argv[1])
	}
	addReplyLongLong(c, deleted)
}

/* smembersCommand: SMEMBERS key
 * 返回全部成员(Set 无序,返回顺序不保证)。
 */
func smembersCommand(c *redisClient) {
	o := lookupKeyReadOrReply(c, c.argv[1], shared.emptymultibulk)
	if o == nil || checkType(c, o, REDIS_SET) {
		return
	}

	addReplyMultiBulkLen(c, setTypeSize(o))
	it := setTypeInitIterator(o)
	for {
		ele, ok := setTypeNext(it)
		if !ok {
			break
		}
		addReplyBulk(c, ele)
	}
}

/* sismemberCommand: SISMEMBER key member
 * 成员存在返回 1,否则 0。
 */
func sismemberCommand(c *redisClient) {
	o := lookupKeyReadOrReply(c, c.argv[1], shared.czero)
	if o == nil || checkType(c, o, REDIS_SET) {
		return
	}

	if setTypeIsMember(o, c.argv[2]) {
		addReply(c, shared.cone)
	} else {
		addReply(c, shared.czero)
	}
}

/* scardCommand: SCARD key
 * 返回集合元素个数。
 */
func scardCommand(c *redisClient) {
	o := lookupKeyReadOrReply(c, c.argv[1], shared.czero)
	if o == nil || checkType(c, o, REDIS_SET) {
		return
	}
	addReplyLongLong(c, setTypeSize(o))
}

/* spopCommand: SPOP key
 * 随机弹出并返回一个成员;弹空后移除 key。
 */
func spopCommand(c *redisClient) {
	o := lookupKeyWriteOrReply(c, c.argv[1], shared.nullbulk)
	if o == nil || checkType(c, o, REDIS_SET) {
		return
	}

	ele, ok := setTypeRandomElement(o)
	if !ok {
		addReply(c, shared.nullbulk)
		return
	}
	setTypeRemove(o, ele)
	if setTypeSize(o) == 0 {
		dbDelete(c.db, c.argv[1])
	}
	addReplyBulk(c, ele)
}

/* ============================ 集合运算 ============================ */

const (
	SET_OP_INTER = iota // 交集
	SET_OP_UNION        // 并集
	SET_OP_DIFF         // 差集
)

/* collectSetObjects 收集 argv[begin..argc) 对应的 set 对象切片。
 * 缺失 key → 对应位置为 nil;类型不符 → 回复 WRONGTYPE 并返回 nil(整体失败)。
 */
func collectSetObjects(c *redisClient, begin uint64) []*robj {
	sets := make([]*robj, 0, c.argc-begin)
	for i := begin; i < c.argc; i++ {
		o := lookupKeyRead(c.db, c.argv[i])
		if o == nil {
			sets = append(sets, nil)
			continue
		}
		if checkType(c, o, REDIS_SET) {
			return nil
		}
		sets = append(sets, o)
	}
	return sets
}

/* outputSetResult 把运算结果(map)按 multibulk 回复,或存入 dstkey 后回复元素数。
 * 结果为空时:回复空 multibulk,或删除 dstkey 后回复 0。
 */
func outputSetResult(c *redisClient, dstkey *robj, result map[string]struct{}) {
	if dstkey != nil {
		// STORE:先删后写,覆盖旧值
		dbDelete(c.db, dstkey)
		if len(result) == 0 {
			addReply(c, shared.czero)
			return
		}
		o := createSetObject()
		m := (*o.ptr).(map[string]struct{})
		for k := range result {
			m[k] = struct{}{}
		}
		dbAdd(c.db, dstkey, o)
		addReplyLongLong(c, int64(len(result)))
		return
	}
	// 非 STORE:multibulk 回复全部元素
	addReplyMultiBulkLen(c, int64(len(result)))
	for k := range result {
		s := k
		addReplyBulk(c, createStringObject(&s, len(s)))
	}
}

/* replyOrStoreEmptyResult 处理"交集/结果为空"的快速路径 */
func replyOrStoreEmptyResult(c *redisClient, dstkey *robj) {
	if dstkey != nil {
		dbDelete(c.db, dstkey)
		addReply(c, shared.czero)
		return
	}
	addReply(c, shared.emptymultibulk)
}

/* isMemberOfAll 判断 ele 是否同时存在于 sets 中除 skipIdx 外的所有集合。
 * 这是 SINTER 的核心判定:驱动集合的某个元素要进入交集结果,
 * 必须同时出现在所有其他集合中。skipIdx 对应驱动集合自身(必含该元素,跳过)。
 */
func isMemberOfAll(sets []*robj, skipIdx int, ele *robj) bool {
	for i, s := range sets {
		if i == skipIdx {
			continue // 跳过驱动集合自身(它必然含 ele)
		}
		// 用 setTypeIsMember 而非直接断言 map —— 否则源集合为 intset 编码时会 panic。
		// 抽象层会按 encoding 自动分发(HT→map 查找,intset→intsetSearch)。
		if !setTypeIsMember(s, ele) {
			return false // 短路:任一集合不含即淘汰
		}
	}
	return true
}

/* sinterGenericCommand 计算 argv[begin..argc) 的交集。
 * dstkey != nil 时存入 dstkey 并回复元素数(SINTERSTORE);否则 multibulk 回复(SINTER)。
 *
 * 关键优化:以最小集合为"驱动"遍历 —— 交集大小 ≤ 最小集合大小,
 * 从最小集合出发才能让逐元素查其他集合的次数最少。
 */
func sinterGenericCommand(c *redisClient, dstkey *robj, begin uint64) {
	sets := collectSetObjects(c, begin)
	if sets == nil {
		return // WRONGTYPE already replied
	}
	// 任意一个集合不存在/为空 → 交集必为空
	for _, s := range sets {
		if s == nil {
			replyOrStoreEmptyResult(c, dstkey)
			return
		}
	}
	// 选最小集合作为驱动
	driverIdx := 0
	for i := 1; i < len(sets); i++ {
		if setTypeSize(sets[i]) < setTypeSize(sets[driverIdx]) {
			driverIdx = i
		}
	}

	result := map[string]struct{}{}
	it := setTypeInitIterator(sets[driverIdx])
	for {
		ele, ok := setTypeNext(it)
		if !ok {
			break
		}
		//如果另外的set都包含该元素,则存入结果集中
		if isMemberOfAll(sets, driverIdx, ele) {
			result[setMemberKey(ele)] = struct{}{}
		}
	}
	outputSetResult(c, dstkey, result)
}

/* sunionDiffGenericCommand 计算并集(op==UNION)或差集(op==DIFF)。
 * - UNION:每个集合的所有成员都加入结果(map 自动去重)。
 * - DIFF:第一个集合为 base,后续集合的成员从 base 中剔除。
 */
func sunionDiffGenericCommand(c *redisClient, op int, dstkey *robj, begin uint64) {
	sets := collectSetObjects(c, begin)
	if sets == nil {
		return
	}
	result := map[string]struct{}{}

	if op == SET_OP_UNION {
		for _, s := range sets {
			if s == nil {
				continue
			}
			it := setTypeInitIterator(s)
			for {
				ele, ok := setTypeNext(it)
				if !ok {
					break
				}
				result[setMemberKey(ele)] = struct{}{}
			}
		}
	} else { // SET_OP_DIFF
		// base 为空(不存在)→ 差集为空
		if len(sets) == 0 || sets[0] == nil {
			replyOrStoreEmptyResult(c, dstkey)
			return
		}
		// 先把 base 全部加入结果
		it := setTypeInitIterator(sets[0])
		for {
			ele, ok := setTypeNext(it)
			if !ok {
				break
			}
			result[setMemberKey(ele)] = struct{}{}
		}
		// 再用后续集合剔除
		for idx := 1; idx < len(sets); idx++ {
			if sets[idx] == nil {
				continue
			}
			it := setTypeInitIterator(sets[idx])
			for {
				ele, ok := setTypeNext(it)
				if !ok {
					break
				}
				delete(result, setMemberKey(ele))
			}
		}
	}

	outputSetResult(c, dstkey, result)
}

/* --- 集合运算命令处理函数 --- */

func sinterCommand(c *redisClient)      { sinterGenericCommand(c, nil, 1) }
func sinterstoreCommand(c *redisClient) { sinterGenericCommand(c, c.argv[1], 2) } // dst=argv[1], 源集合从 argv[2]
func sunionCommand(c *redisClient)      { sunionDiffGenericCommand(c, SET_OP_UNION, nil, 1) }
func sunionstoreCommand(c *redisClient) { sunionDiffGenericCommand(c, SET_OP_UNION, c.argv[1], 2) }
func sdiffCommand(c *redisClient)       { sunionDiffGenericCommand(c, SET_OP_DIFF, nil, 1) }
func sdiffstoreCommand(c *redisClient)  { sunionDiffGenericCommand(c, SET_OP_DIFF, c.argv[1], 2) }
