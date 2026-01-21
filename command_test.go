package main

import (
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
