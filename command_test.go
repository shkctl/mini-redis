package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

// setupTest 初始化测试环境
// 初始化共享对象和服务器
func setupTest() {
	// 初始化共享对象
	createSharedObjects()

	// 初始化服务器配置
	loadServerConfig()
	server.dbnum = 1
	initServerConfig()
	initServer()
}

// listCommonTest 测试链表相关命令的功能
// 包含以下测试用例：
// 1. 测试连续三次添加元素aa、bb、cc后，链表长度为3
// 2. 测试索引0的元素为aa
// 3. 测试索引1的元素为bb
// 4. 测试索引2的元素为cc
// 5. 测试弹出第一个元素后，链表头为bb
// 6. 测试再次弹出元素后，链表长度为1
func listCommonTest(t *testing.T) {
	// 初始化测试环境
	setupTest()

	// 测试1: 连续三次添加元素aa、bb、cc后，链表长度为3
	t.Log("测试1: 连续三次添加元素aa、bb、cc后，链表长度为3")

	// 创建链表对象
	listObj := createListObject()

	// 添加元素aa
	val1 := "aa"
	obj1 := createStringObject(&val1, len(val1))
	listTypePush(listObj, obj1, REDIS_TAIL)

	// 添加元素bb
	val2 := "bb"
	obj2 := createStringObject(&val2, len(val2))
	listTypePush(listObj, obj2, REDIS_TAIL)

	// 添加元素cc
	val3 := "cc"
	obj3 := createStringObject(&val3, len(val3))
	listTypePush(listObj, obj3, REDIS_TAIL)

	// 检查链表长度是否为3
	length := listTypeLength(listObj)
	if length != 3 {
		t.Fatalf("链表长度应为3，实际为%d", length)
	}
	t.Logf("测试1通过: 链表长度为%d", length)

	// 测试2: 测试索引0的元素为aa
	t.Log("测试2: 测试索引0的元素为aa")
	l := (*listObj.ptr).(*list)
	node := listIndex(l, 0)
	if node == nil {
		t.Fatal("索引0的节点不存在")
	}
	val := (*(*node.value).(*robj).ptr).(string)
	if val != "aa" {
		t.Fatalf("索引0的元素应为 'aa'，实际为 '%s'", val)
	}
	t.Log("测试2通过: 索引0的元素为 aa")

	// 测试3: 测试索引1的元素为bb
	t.Log("测试3: 测试索引1的元素为bb")
	node = listIndex(l, 1)
	if node == nil {
		t.Fatal("索引1的节点不存在")
	}
	val = (*(*node.value).(*robj).ptr).(string)
	if val != "bb" {
		t.Fatalf("索引1的元素应为 'bb'，实际为 '%s'", val)
	}
	t.Log("测试3通过: 索引1的元素为 bb")

	// 测试4: 测试索引2的元素为cc
	t.Log("测试4: 测试索引2的元素为cc")
	node = listIndex(l, 2)
	if node == nil {
		t.Fatal("索引2的节点不存在")
	}
	val = (*(*node.value).(*robj).ptr).(string)
	if val != "cc" {
		t.Fatalf("索引2的元素应为 'cc'，实际为 '%s'", val)
	}
	t.Log("测试4通过: 索引2的元素为 cc")

	// 测试5: 测试弹出第一个元素后，链表头为bb
	t.Log("测试5: 测试弹出第一个元素后，链表头为bb")
	listTypePop(listObj, REDIS_HEAD)

	// 检查链表头节点是否为bb
	if l.head == nil {
		t.Fatal("链表头节点不存在")
	}
	val = (*(*l.head.value).(*robj).ptr).(string)
	if val != "bb" {
		t.Fatalf("弹出第一个元素后，链表头应为 'bb'，实际为 '%s'", val)
	}
	t.Log("测试5通过: 弹出第一个元素后，链表头为 bb")

	// 测试6: 测试再次弹出元素后，链表长度为1
	t.Log("测试6: 测试再次弹出元素后，链表长度为1")
	listTypePop(listObj, REDIS_HEAD)

	// 检查链表长度是否为1
	length = listTypeLength(listObj)
	if length != 1 {
		t.Fatalf("链表长度应为1，实际为%d", length)
	}

	// 检查链表头节点是否为cc
	val = (*(*l.head.value).(*robj).ptr).(string)
	if val != "cc" {
		t.Fatalf("再次弹出元素后，链表头应为 'cc'，实际为 '%s'", val)
	}
	t.Logf("测试6通过: 链表长度为%d", length)

	t.Log("所有测试用例通过！")
}

// TestListCommands 执行链表相关命令的测试
func TestListCommands(t *testing.T) {
	listCommonTest(t)
}

// --- SCAN 命令集成测试 ---

// newClientWithPipe 用 net.Pipe 构造一个可记录回复的测试 client
// 返回 client、server 端 conn,以及读取 server 端输出的 reader
func newClientWithPipe(t *testing.T) (*redisClient, net.Conn, *bufio.Reader) {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	c := &redisClient{conn: serverSide}
	return c, clientSide, bufio.NewReader(clientSide)
}

// runScanArgv 构造 c.argv/argc 并触发 scanCommand
// 在独立 goroutine 中调用,以便测试代码同步读取 pipe
func runScanArgv(t *testing.T, c *redisClient, args ...string) {
	t.Helper()
	c.argv = make([]*robj, len(args))
	for i, a := range args {
		s := a
		c.argv[i] = createStringObject(&s, len(s))
	}
	c.argc = uint64(len(args))
	go func() {
		scanCommand(c)
	}()
}

// readMultiBulk 解析 RESP `*N\r\n` 数组,返回原始字符串数组
// 数组元素可能是另一个数组(嵌套),也可能是 bulk 字符串
func readMultiBulk(t *testing.T, r *bufio.Reader) []interface{} {
	t.Helper()
	header, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read header failed: %v", err)
	}
	if len(header) < 4 || header[0] != '*' {
		t.Fatalf("not a multibulk header: %q", header)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(header[1:], "\n"), "\r"))
	if err != nil {
		t.Fatalf("parse length failed: %v", err)
	}
	out := make([]interface{}, n)
	for i := 0; i < n; i++ {
		peek, err := r.Peek(1)
		if err != nil {
			t.Fatalf("peek failed: %v", err)
		}
		if peek[0] == '*' {
			out[i] = readMultiBulk(t, r)
			continue
		}
		// bulk string: $<len>\r\n<payload>\r\n
		bh, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read bulk header failed: %v", err)
		}
		if bh[0] != '$' {
			t.Fatalf("expected bulk header, got %q", bh)
		}
		bl, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(bh[1:], "\n"), "\r"))
		if err != nil {
			t.Fatalf("parse bulk length failed: %v", err)
		}
		buf := make([]byte, bl)
		if _, err := r.Read(buf); err != nil {
			t.Fatalf("read bulk body failed: %v", err)
		}
		// 消费末尾 \r\n
		if _, err := r.Discard(2); err != nil {
			t.Fatalf("discard crlf failed: %v", err)
		}
		out[i] = string(buf)
	}
	return out
}

// readErrorReply 读取一行 `-ERR ...\r\n` 错误回复
func readErrorReply(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read error line failed: %v", err)
	}
	if len(line) == 0 || line[0] != '-' {
		t.Fatalf("expected error reply, got %q", line)
	}
	return strings.TrimSuffix(strings.TrimSuffix(line[1:], "\n"), "\r")
}

// TestScanCommand 验证 SCAN 命令的关键路径:
//   - 基础形态:SCAN 0 → 返回 [cursor, [keys...]] 数组
//   - 模式过滤:SCAN 0 MATCH user:* COUNT 100
//   - 错误处理:invalid cursor、未知选项
func TestScanCommand(t *testing.T) {
	setupTest()

	// 在 db[0] 中插入 50 个 key,其中 10 个为 user:* 前缀
	db := &server.db[0]
	for i := 0; i < 50; i++ {
		var key string
		if i < 10 {
			key = fmt.Sprintf("user:%d", i)
		} else {
			key = fmt.Sprintf("other:%d", i)
		}
		val := "v"
		dbAdd(db, createStringObject(&key, len(key)), createStringObject(&val, len(val)))
	}

	t.Run("basic SCAN 0 returns cursor and keys", func(t *testing.T) {
		c, clientConn, r := newClientWithPipe(t)
		defer clientConn.Close()
		c.db = db

		runScanArgv(t, c, "SCAN", "0")
		reply := readMultiBulk(t, r)
		if len(reply) != 2 {
			t.Fatalf("SCAN reply should have 2 elements, got %d", len(reply))
		}
		cursorStr, ok := reply[0].(string)
		if !ok {
			t.Fatalf("cursor should be bulk string, got %T", reply[0])
		}
		if _, err := strconv.ParseUint(cursorStr, 10, 64); err != nil {
			t.Fatalf("cursor not a valid uint64: %q", cursorStr)
		}
		keys, ok := reply[1].([]interface{})
		if !ok {
			t.Fatalf("keys element should be array, got %T", reply[1])
		}
		// 至少返回一些 key(默认 count=10);具体数量与桶分布相关
		if len(keys) == 0 {
			t.Fatalf("SCAN 0 should return at least one key from 50-key db")
		}
	})

	t.Run("MATCH filters by glob pattern", func(t *testing.T) {
		c, clientConn, r := newClientWithPipe(t)
		defer clientConn.Close()
		c.db = db

		// COUNT 200 让一次调用尽量扫到全部桶
		runScanArgv(t, c, "SCAN", "0", "MATCH", "user:*", "COUNT", "200")
		reply := readMultiBulk(t, r)
		keys := reply[1].([]interface{})
		// 所有返回的 key 必须命中 user: 前缀
		for _, item := range keys {
			s := item.(string)
			if !strings.HasPrefix(s, "user:") {
				t.Fatalf("MATCH user:* leaked non-matching key: %q", s)
			}
		}
	})

	t.Run("invalid cursor returns error", func(t *testing.T) {
		c, clientConn, r := newClientWithPipe(t)
		defer clientConn.Close()
		c.db = db

		runScanArgv(t, c, "SCAN", "not-a-number")
		msg := readErrorReply(t, r)
		if !strings.Contains(msg, "invalid cursor") {
			t.Fatalf("expected invalid cursor error, got %q", msg)
		}
	})

	t.Run("unknown option returns syntax error", func(t *testing.T) {
		c, clientConn, r := newClientWithPipe(t)
		defer clientConn.Close()
		c.db = db

		runScanArgv(t, c, "SCAN", "0", "BOGUS", "x")
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		// shared.syntaxerr 直接写入 conn,首字符为 `-`(错误)或共享对象格式
		if !strings.Contains(line, "syntax") && !strings.HasPrefix(line, "-") {
			t.Fatalf("expected syntax error reply, got %q", line)
		}
	})
}
