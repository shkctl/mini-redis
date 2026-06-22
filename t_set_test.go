package main

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

/* ---- 测试基建:mock conn 捕获响应 + 构造 client ---- */

// mockConn 捕获所有 Write 字节,模拟一个客户端连接
type mockConn struct {
	buf []byte
}

func (m *mockConn) Read(b []byte) (int, error)       { return 0, nil }
func (m *mockConn) Write(b []byte) (int, error)      { m.buf = append(m.buf, b...); return len(b), nil }
func (m *mockConn) Close() error                     { return nil }
func (m *mockConn) LocalAddr() net.Addr              { return nil }
func (m *mockConn) RemoteAddr() net.Addr             { return nil }
func (m *mockConn) SetDeadline(time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(time.Time) error { return nil }

// setupSetTest 初始化 server(每次调用都重建 server.db,保证测试间 key 隔离)
func setupSetTest() {
	createSharedObjects()
	loadServerConfig()
	server.dbnum = 1
	initServerConfig()
	initServer()
}

// newSetClient 用给定参数构造一个绑定了 mock conn 的 redisClient
func newSetClient(db *redisDb, args ...string) (*redisClient, *mockConn) {
	argv := make([]*robj, len(args))
	for i, a := range args {
		s := a
		argv[i] = createStringObject(&s, len(s))
	}
	mc := &mockConn{}
	c := &redisClient{
		argc: uint64(len(args)),
		argv: argv,
		db:   db,
		conn: mc,
	}
	return c, mc
}

// reply 取出 mock conn 累积的响应字符串
func reply(mc *mockConn) string {
	return string(mc.buf)
}

func db0() *redisDb { return &server.db[0] }

/* ============================ SADD / SCARD / SISMEMBER ============================ */

// 基本正向:整数成员走 intset,SADD 返回新增数,SCARD/SISMEMBER 正确
func TestSet_SADD_Basic(t *testing.T) {
	setupSetTest()

	c, mc := newSetClient(db0(), "SADD", "k", "1", "2", "3")
	saddCommand(c)
	if r := reply(mc); !strings.Contains(r, ":3\r\n") {
		t.Fatalf("SADD 应返回 :3, 实际 %q", r)
	}

	// SCARD = 3
	c2, mc2 := newSetClient(db0(), "SCARD", "k")
	scardCommand(c2)
	if r := reply(mc2); !strings.Contains(r, ":3\r\n") {
		t.Fatalf("SCARD 应返回 :3, 实际 %q", r)
	}

	// SISMEMBER 2 → 1, 9 → 0
	c3, mc3 := newSetClient(db0(), "SISMEMBER", "k", "2")
	sismemberCommand(c3)
	if !strings.Contains(reply(mc3), ":1\r\n") {
		t.Errorf("SISMEMBER 2 应为 1")
	}
	c4, mc4 := newSetClient(db0(), "SISMEMBER", "k", "9")
	sismemberCommand(c4)
	if !strings.Contains(reply(mc4), ":0\r\n") {
		t.Errorf("SISMEMBER 9 应为 0")
	}
}

// 去重:重复 SADD 同一元素,返回的新增数不计重复
func TestRedisCompat_SADD_Dedup(t *testing.T) {
	setupSetTest()

	c, mc := newSetClient(db0(), "SADD", "k", "1", "1", "2")
	saddCommand(c)
	if !strings.Contains(reply(mc), ":2\r\n") {
		t.Fatalf("SADD 1 1 2 应只新增 2 个, 实际 %q", reply(mc))
	}
}

// 编码升级:intset 插入非整数成员 → 自动转 HT,后续操作仍正确
func TestSet_EncodingUpgrade_NonInteger(t *testing.T) {
	setupSetTest()

	// 先建 intset(整数 1)
	c, _ := newSetClient(db0(), "SADD", "k", "1")
	saddCommand(c)
	o := lookupKeyWrite(db0(), c.argv[1])
	if o.encoding != REDIS_ENCODING_INTSET {
		t.Fatalf("初始应为 INTSET, 实际 %d", o.encoding)
	}

	// 插入非整数 → 升级为 HT
	c2, _ := newSetClient(db0(), "SADD", "k", "abc")
	saddCommand(c2)
	o = lookupKeyWrite(db0(), c.argv[1])
	if o.encoding != REDIS_ENCODING_HT {
		t.Fatalf("插入非整数后应为 HT, 实际 %d", o.encoding)
	}

	// 升级后整数成员 1 仍在(跨编码成员判定一致)
	c3, mc3 := newSetClient(db0(), "SISMEMBER", "k", "1")
	sismemberCommand(c3)
	if !strings.Contains(reply(mc3), ":1\r\n") {
		t.Errorf("升级后整数成员 1 应仍存在")
	}
	c4, mc4 := newSetClient(db0(), "SISMEMBER", "k", "abc")
	sismemberCommand(c4)
	if !strings.Contains(reply(mc4), ":1\r\n") {
		t.Errorf("升级后字符串成员 abc 应存在")
	}
}

// 编码升级:元素数超过 512 阈值 → intset 转 HT
func TestSet_EncodingUpgrade_Threshold(t *testing.T) {
	setupSetTest()

	o := setTypeCreate(createStringObjectFromString("1"))
	dbAdd(db0(), createStringObjectFromString("k"), o)

	for i := 1; i <= 513; i++ {
		member := createStringObjectFromString(strconv.Itoa(i))
		setTypeAdd(o, member)
	}
	if o.encoding != REDIS_ENCODING_HT {
		t.Fatalf("超过 512 阈值应升级为 HT, 实际 %d", o.encoding)
	}
	if got := setTypeSize(o); got != 513 {
		t.Errorf("元素数 = %d, want 513", got)
	}
}

/* ============================ SMEMBERS ============================ */

// SMEMBERS 返回 multibulk,成员数正确
func TestSet_SMEMBERS_Basic(t *testing.T) {
	setupSetTest()

	c, _ := newSetClient(db0(), "SADD", "k", "a", "b", "c")
	saddCommand(c)

	c2, mc2 := newSetClient(db0(), "SMEMBERS", "k")
	smembersCommand(c2)
	r := reply(mc2)
	if !strings.HasPrefix(r, "*3\r\n") {
		t.Fatalf("SMEMBERS 应以 *3 开头, 实际 %q", r)
	}
	// 三个成员都应出现
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(r, "$1\r\n"+want+"\r\n") {
			t.Errorf("SMEMBERS 结果缺少 %q, 完整 %q", want, r)
		}
	}
}

// SMEMBERS 对不存在的 key 返回空 multibulk
func TestSet_SMEMBERS_MissingKey(t *testing.T) {
	setupSetTest()
	c, mc := newSetClient(db0(), "SMEMBERS", "nope")
	smembersCommand(c)
	if r := reply(mc); !strings.Contains(r, "*0\r\n") {
		t.Fatalf("不存在 key 应返回 *0, 实际 %q", r)
	}
}

/* ============================ SREM ============================ */

// SREM 删除并计数;删空后 key 被移除
func TestSet_SREM_Basic(t *testing.T) {
	setupSetTest()

	add, _ := newSetClient(db0(), "SADD", "k", "1", "2", "3")
	saddCommand(add)

	c, mc := newSetClient(db0(), "SREM", "k", "2", "9")
	sremCommand(c)
	if !strings.Contains(reply(mc), ":1\r\n") {
		t.Fatalf("SREM 2 9 应只删 1 个, 实际 %q", reply(mc))
	}

	// 2 已删
	c2, mc2 := newSetClient(db0(), "SISMEMBER", "k", "2")
	sismemberCommand(c2)
	if !strings.Contains(reply(mc2), ":0\r\n") {
		t.Errorf("SREM 后 2 应不存在")
	}
}

// SREM 删空 → key 被删除(SCARD 返回 0,key 真正消失)
func TestSet_SREM_EmptyKeyDeleted(t *testing.T) {
	setupSetTest()

	add, _ := newSetClient(db0(), "SADD", "k", "1")
	saddCommand(add)

	c, _ := newSetClient(db0(), "SREM", "k", "1")
	sremCommand(c)

	// key 应已被 dbDelete
	if lookupKeyWrite(db0(), c.argv[1]) != nil {
		t.Error("SREM 删空后 key 应被移除")
	}
}

/* ============================ SPOP ============================ */

// SPOP 弹出一个成员,集合 size 减 1
func TestSet_SPOP_Basic(t *testing.T) {
	setupSetTest()

	add, _ := newSetClient(db0(), "SADD", "k", "1", "2")
	saddCommand(add)

	c, mc := newSetClient(db0(), "SPOP", "k")
	spopCommand(c)
	r := reply(mc)
	// 应返回一个 bulk 成员(1 或 2)
	if !(strings.Contains(r, "$1\r\n1\r\n") || strings.Contains(r, "$1\r\n2\r\n")) {
		t.Fatalf("SPOP 应弹出 1 或 2, 实际 %q", r)
	}
	// size 应为 1
	c2, mc2 := newSetClient(db0(), "SCARD", "k")
	scardCommand(c2)
	if !strings.Contains(reply(mc2), ":1\r\n") {
		t.Errorf("SPOP 后 SCARD 应为 1")
	}
}

// SPOP 对不存在 key 返回 nil bulk
func TestSet_SPOP_MissingKey(t *testing.T) {
	setupSetTest()
	c, mc := newSetClient(db0(), "SPOP", "nope")
	spopCommand(c)
	if !strings.Contains(reply(mc), "$-1\r\n") {
		t.Fatalf("SPOP 不存在 key 应返回 $-1, 实际 %q", reply(mc))
	}
}

/* ============================ WRONGTYPE ============================ */

// 对非 SET 类型的 key 执行 SET 命令 → WRONGTYPE
func TestSet_WrongType(t *testing.T) {
	setupSetTest()

	// 先放一个 STRING
	add, _ := newSetClient(db0(), "SET", "k", "v")
	setCommand(add)

	c, mc := newSetClient(db0(), "SADD", "k", "1")
	saddCommand(c)
	if !strings.Contains(reply(mc), "WRONGTYPE") {
		t.Fatalf("对 STRING 执行 SADD 应返回 WRONGTYPE, 实际 %q", reply(mc))
	}
}

/* ============================ 辅助 ============================ */

func createStringObjectFromString(s string) *robj {
	str := s
	return createStringObject(&str, len(str))
}

// bulkContains 判断 RESP multibulk 响应里是否包含指定成员的 bulk
func bulkContains(r, member string) bool {
	pat := "$" + strconv.Itoa(len(member)) + "\r\n" + member + "\r\n"
	return strings.Contains(r, pat)
}

// mustSCARD 返回某 key 的元素数(用于 STORE 后的校验)
func mustSCARD(key string) int64 {
	c, _ := newSetClient(db0(), "SCARD", key)
	scardCommand(c)
	// 直接读 setTypeSize,避免解析回复
	o := lookupKeyRead(db0(), createStringObjectFromString(key))
	if o == nil {
		return 0
	}
	return setTypeSize(o)
}

/* ============================ SINTER ============================ */

// 基本交集(整数源集合,触发 intset 编码 —— 也是 isMemberOfAll 修复的回归点)
func TestSet_SINTER_Basic(t *testing.T) {
	setupSetTest()
	newSetClientRun := func(args ...string) (*redisClient, *mockConn) {
		return newSetClient(db0(), args...)
	}

	c, _ := newSetClientRun("SADD", "a", "1", "2", "3")
	saddCommand(c)
	c, _ = newSetClientRun("SADD", "b", "2", "3", "4")
	saddCommand(c)

	c, mc := newSetClientRun("SINTER", "a", "b")
	sinterCommand(c)
	r := reply(mc)
	if !strings.HasPrefix(r, "*2\r\n") {
		t.Fatalf("SINTER a b 元素数应为 2, 实际 %q", r)
	}
	if !bulkContains(r, "2") || !bulkContains(r, "3") {
		t.Errorf("SINTER a b 应含 2 和 3, 实际 %q", r)
	}
	if bulkContains(r, "1") || bulkContains(r, "4") {
		t.Errorf("SINTER a b 不应含 1 或 4, 实际 %q", r)
	}
}

// 混合编码交集:intset 源 ∩ HT 源(验证 setTypeIsMember 跨编码分发)
func TestSet_SINTER_MixedEncoding(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	// a = intset(纯整数),b = HT(含非整数 "x")
	c, _ := run("SADD", "a", "1", "2", "3")
	saddCommand(c)
	c, _ = run("SADD", "b", "x", "2", "3")
	saddCommand(c)

	c, mc := run("SINTER", "a", "b")
	sinterCommand(c)
	r := reply(mc)
	if !strings.HasPrefix(r, "*2\r\n") {
		t.Fatalf("混合编码 SINTER 元素数应为 2, 实际 %q", r)
	}
	if !bulkContains(r, "2") || !bulkContains(r, "3") {
		t.Errorf("应含 2 和 3, 实际 %q", r)
	}
}

// 交集为空:源集合缺失 → 返回空 multibulk
func TestSet_SINTER_EmptyResult(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2", "3")
	saddCommand(c)

	c, mc := run("SINTER", "a", "nope") // nope 不存在 → 交集为空
	sinterCommand(c)
	if !strings.Contains(reply(mc), "*0\r\n") {
		t.Fatalf("源集合缺失应返回 *0, 实际 %q", reply(mc))
	}
}

// SINTERSTORE:结果存入 dst,返回元素数
func TestSet_SINTERSTORE(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2", "3")
	saddCommand(c)
	c, _ = run("SADD", "b", "2", "3", "4")
	saddCommand(c)

	c, mc := run("SINTERSTORE", "dst", "a", "b")
	sinterstoreCommand(c)
	if !strings.Contains(reply(mc), ":2\r\n") {
		t.Fatalf("SINTERSTORE 应回复 :2, 实际 %q", reply(mc))
	}
	if got := mustSCARD("dst"); got != 2 {
		t.Errorf("dst 元素数 = %d, want 2", got)
	}
	// 校验 dst 内容 = {2,3}
	c, mc = run("SMEMBERS", "dst")
	smembersCommand(c)
	r := reply(mc)
	if !bulkContains(r, "2") || !bulkContains(r, "3") {
		t.Errorf("dst 应含 2 和 3, 实际 %q", r)
	}
}

/* ============================ SUNION ============================ */

func TestSet_SUNION_Basic(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2")
	saddCommand(c)
	c, _ = run("SADD", "b", "2", "3")
	saddCommand(c)

	c, mc := run("SUNION", "a", "b")
	sunionCommand(c)
	r := reply(mc)
	if !strings.HasPrefix(r, "*3\r\n") {
		t.Fatalf("SUNION a b 元素数应为 3(1,2,3 去重后), 实际 %q", r)
	}
	for _, m := range []string{"1", "2", "3"} {
		if !bulkContains(r, m) {
			t.Errorf("SUNION 应含 %s, 实际 %q", m, r)
		}
	}
}

func TestSet_SUNIONSTORE(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2")
	saddCommand(c)
	c, _ = run("SADD", "b", "2", "3")
	saddCommand(c)

	c, mc := run("SUNIONSTORE", "dst", "a", "b")
	sunionstoreCommand(c)
	if !strings.Contains(reply(mc), ":3\r\n") {
		t.Fatalf("SUNIONSTORE 应回复 :3, 实际 %q", reply(mc))
	}
	if got := mustSCARD("dst"); got != 3 {
		t.Errorf("dst 元素数 = %d, want 3", got)
	}
}

/* ============================ SDIFF ============================ */

func TestSet_SDIFF_Basic(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2", "3")
	saddCommand(c)
	c, _ = run("SADD", "b", "2", "3", "4")
	saddCommand(c)

	c, mc := run("SDIFF", "a", "b") // a - b = {1}
	sdiffCommand(c)
	r := reply(mc)
	if !strings.HasPrefix(r, "*1\r\n") {
		t.Fatalf("SDIFF a b 元素数应为 1, 实际 %q", r)
	}
	if !bulkContains(r, "1") {
		t.Errorf("SDIFF a b 应含 1, 实际 %q", r)
	}
	if bulkContains(r, "2") || bulkContains(r, "3") || bulkContains(r, "4") {
		t.Errorf("SDIFF a b 不应含 2/3/4, 实际 %q", r)
	}
}

func TestSet_SDIFFSTORE(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SADD", "a", "1", "2", "3")
	saddCommand(c)
	c, _ = run("SADD", "b", "2", "3", "4")
	saddCommand(c)

	c, mc := run("SDIFFSTORE", "dst", "a", "b")
	sdiffstoreCommand(c)
	if !strings.Contains(reply(mc), ":1\r\n") {
		t.Fatalf("SDIFFSTORE 应回复 :1, 实际 %q", reply(mc))
	}
	if got := mustSCARD("dst"); got != 1 {
		t.Errorf("dst 元素数 = %d, want 1", got)
	}
}

// 集合运算中的 WRONGTYPE
func TestSet_OpsWrongType(t *testing.T) {
	setupSetTest()
	run := func(args ...string) (*redisClient, *mockConn) { return newSetClient(db0(), args...) }

	c, _ := run("SET", "str", "v") // str 是 STRING 类型
	setCommand(c)
	c, _ = run("SADD", "s", "1")
	saddCommand(c)

	c, mc := run("SINTER", "s", "str") // str 不是 SET → WRONGTYPE
	sinterCommand(c)
	if !strings.Contains(reply(mc), "WRONGTYPE") {
		t.Fatalf("SINTER 含非 SET key 应返回 WRONGTYPE, 实际 %q", reply(mc))
	}
}
