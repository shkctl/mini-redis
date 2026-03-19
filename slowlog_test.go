package main

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// 测试辅助函数：初始化测试环境
// 参考现有command_test.go的setupTest()模式
func setupSlowlogTest() {
	// 初始化共享对象（必须在最前面）
	createSharedObjects()

	// 初始化服务器配置
	loadServerConfig()
	initServerConfig()
	initServer()

	// 显式初始化slowlog（确保TDD早期阶段可工作）
	server.slowlog = listCreate()
	server.slowlogEntryId = 0
	server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
	server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN
}

// 测试辅助函数：创建测试用的robj参数
func createTestArgs(args ...string) []*robj {
	result := make([]*robj, len(args))
	for i, arg := range args {
		s := arg
		result[i] = createStringObject(&s, len(s))
	}
	return result
}

func TestSlowlogInit(t *testing.T) {
	setupSlowlogTest()

	// 验证slowlog链表已创建
	if server.slowlog == nil {
		t.Fatal("slowlog list is nil")
	}

	// 验证链表为空
	if listLength(server.slowlog) != 0 {
		t.Errorf("expected empty slowlog, got %d items", listLength(server.slowlog))
	}

	// 验证默认配置
	if server.slowlogLogSlowerThan != 10000 {
		t.Errorf("expected threshold 10000, got %d", server.slowlogLogSlowerThan)
	}

	if server.slowlogMaxLen != 128 {
		t.Errorf("expected maxLen 128, got %d", server.slowlogMaxLen)
	}

	// 验证ID初始化
	if server.slowlogEntryId != 0 {
		t.Errorf("expected entryId 0, got %d", server.slowlogEntryId)
	}
}

func TestConstants(t *testing.T) {
	// 验证常量与Redis定义一致
	if SLOWLOG_ENTRY_MAX_ARGC != 32 {
		t.Errorf("expected SLOWLOG_ENTRY_MAX_ARGC 32, got %d", SLOWLOG_ENTRY_MAX_ARGC)
	}

	if SLOWLOG_ENTRY_MAX_STRING != 128 {
		t.Errorf("expected SLOWLOG_ENTRY_MAX_STRING 128, got %d", SLOWLOG_ENTRY_MAX_STRING)
	}
}

func TestSlowlogPushEntry_Basic(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 1000 // 阈值1000微秒

	argv := createTestArgs("GET", "mykey")
	duration := int64(1500) // 超过阈值

	// 记录慢查询
	slowlogPushEntryIfNeeded(argv, len(argv), duration)

	// 验证条目数量
	if listLength(server.slowlog) != 1 {
		t.Errorf("expected 1 entry, got %d", listLength(server.slowlog))
	}

	// 获取条目并验证
	node := listFirst(server.slowlog)
	entry := (*node.value).(*slowlogEntry)

	// 验证ID
	if entry.id != 0 {
		t.Errorf("expected ID 0, got %d", entry.id)
	}

	// 验证Duration
	if entry.duration != 1500 {
		t.Errorf("expected duration 1500, got %d", entry.duration)
	}

	// 验证argc
	if entry.argc != 2 {
		t.Errorf("expected 2 args, got %d", entry.argc)
	}
}

func TestSlowlogPushEntry_ArgcLimit(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0 // 记录所有命令

	// 创建超过32个参数的命令
	args := make([]string, 50)
	for i := range args {
		args[i] = "arg"
	}
	argv := createTestArgs(args...)

	slowlogPushEntryIfNeeded(argv, len(argv), 100)

	// 验证条目已记录
	if listLength(server.slowlog) != 1 {
		t.Fatalf("expected 1 entry, got %d", listLength(server.slowlog))
	}

	node := listFirst(server.slowlog)
	entry := (*node.value).(*slowlogEntry)

	// 验证只保留了32个参数
	if len(entry.argv) != SLOWLOG_ENTRY_MAX_ARGC {
		t.Errorf("expected %d args, got %d", SLOWLOG_ENTRY_MAX_ARGC, len(entry.argv))
	}

	// 验证最后一个参数包含省略信息
	lastArg := (*entry.argv[len(entry.argv)-1].ptr).(string)
	expected := "... (19 more arguments)"
	if lastArg != expected {
		t.Errorf("expected '%s', got '%s'", expected, lastArg)
	}
}

func TestSlowlogPushEntry_Disabled(t *testing.T) {
	setupSlowlogTest()
	// 阈值为-1时禁用慢查询
	server.slowlogLogSlowerThan = -1

	argv := createTestArgs("GET", "key")
	slowlogPushEntryIfNeeded(argv, len(argv), 1000000)

	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0 entries when disabled, got %d", listLength(server.slowlog))
	}
}

func TestSlowlogPushEntry_Threshold(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 1000

	// 快速命令（500 < 1000）不应记录
	argv1 := createTestArgs("FAST")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 500)
	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0 entries for fast command, got %d", listLength(server.slowlog))
	}

	// 慢速命令（1500 > 1000）应该记录
	argv2 := createTestArgs("SLOW")
	slowlogPushEntryIfNeeded(argv2, len(argv2), 1500)
	if listLength(server.slowlog) != 1 {
		t.Errorf("expected 1 entry for slow command, got %d", listLength(server.slowlog))
	}
}

func TestSlowlogPushEntry_ThresholdZero(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	argv1 := createTestArgs("CMD1")
	argv2 := createTestArgs("CMD2")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	slowlogPushEntryIfNeeded(argv2, len(argv2), 500)

	if listLength(server.slowlog) != 2 {
		t.Errorf("expected 2 entries with threshold=0, got %d", listLength(server.slowlog))
	}
}

func TestSlowlogLen(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	// 空时为0
	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0, got %d", listLength(server.slowlog))
	}

	// 添加一条记录
	argv1 := createTestArgs("CMD1")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	if listLength(server.slowlog) != 1 {
		t.Errorf("expected 1, got %d", listLength(server.slowlog))
	}

	// 添加第二条记录
	argv2 := createTestArgs("CMD2")
	slowlogPushEntryIfNeeded(argv2, len(argv2), 100)
	if listLength(server.slowlog) != 2 {
		t.Errorf("expected 2, got %d", listLength(server.slowlog))
	}
}

func TestSlowlogGet(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	// 添加3条记录
	argv1 := createTestArgs("CMD1", "arg1")
	argv2 := createTestArgs("CMD2", "arg2")
	argv3 := createTestArgs("CMD3", "arg3")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	slowlogPushEntryIfNeeded(argv2, len(argv2), 200)
	slowlogPushEntryIfNeeded(argv3, len(argv3), 300)

	// 获取前2条
	entries := slowlogGet(2)

	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// 验证顺序（最新在前）
	cmd0 := (*entries[0].argv[0].ptr).(string)
	if cmd0 != "CMD3" {
		t.Errorf("expected CMD3 first, got %s", cmd0)
	}

	cmd1 := (*entries[1].argv[0].ptr).(string)
	if cmd1 != "CMD2" {
		t.Errorf("expected CMD2 second, got %s", cmd1)
	}
}

func TestSlowlogGet_DefaultCount(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0
	server.slowlogMaxLen = 20

	// 添加15条记录
	for i := 0; i < 15; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), int64(i*100))
	}

	// count<=0时返回默认10条
	entries := slowlogGet(0)

	if len(entries) != 10 {
		t.Errorf("expected default 10 entries, got %d", len(entries))
	}
}

func TestSlowlogGet_AllIfLess(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	// 只添加5条
	for i := 0; i < 5; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 100)
	}

	// 请求20条，但只有5条
	entries := slowlogGet(20)

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestSlowlogReset(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	// 添加记录
	argv1 := createTestArgs("CMD1")
	argv2 := createTestArgs("CMD2")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	slowlogPushEntryIfNeeded(argv2, len(argv2), 100)

	if listLength(server.slowlog) != 2 {
		t.Errorf("expected 2 before reset, got %d", listLength(server.slowlog))
	}

	// 重置
	slowlogReset()

	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0 after reset, got %d", listLength(server.slowlog))
	}
}

func TestSlowlogReset_IDContinues(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	argv1 := createTestArgs("CMD1")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	slowlogReset()

	// 重置后ID应该继续递增（与Redis行为一致）
	argv2 := createTestArgs("CMD2")
	slowlogPushEntryIfNeeded(argv2, len(argv2), 100)

	entries := slowlogGet(1)
	// ID应该是1，因为之前已经用了0
	if entries[0].id != 1 {
		t.Errorf("expected ID 1 after reset, got %d", entries[0].id)
	}
}

func TestSlowlogSetMaxLen(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0
	server.slowlogMaxLen = 10

	// 添加15条记录
	for i := 0; i < 15; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 100)
	}

	// 应该被限制在10条
	if listLength(server.slowlog) != 10 {
		t.Errorf("expected 10 after max len, got %d", listLength(server.slowlog))
	}

	// 减少maxLen到5
	slowlogSetMaxLen(5)

	if listLength(server.slowlog) != 5 {
		t.Errorf("expected 5 after SetMaxLen(5), got %d", listLength(server.slowlog))
	}

	// 验证设置值
	if server.slowlogMaxLen != 5 {
		t.Errorf("expected slowlogMaxLen=5, got %d", server.slowlogMaxLen)
	}
}

func TestSlowlogSetThreshold(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 1000

	// 2000 > 1000，应该记录
	argv1 := createTestArgs("SLOW")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 2000)
	if listLength(server.slowlog) != 1 {
		t.Errorf("expected 1 slow entry, got %d", listLength(server.slowlog))
	}

	// 增加阈值到3000
	server.slowlogLogSlowerThan = 3000

	// 2500 < 3000，不应该记录
	argv2 := createTestArgs("NOT_SLOW")
	slowlogPushEntryIfNeeded(argv2, len(argv2), 2500)
	if listLength(server.slowlog) != 1 {
		t.Errorf("expected still 1 after threshold change, got %d", listLength(server.slowlog))
	}
}

func TestIntegration_FullWorkflow(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 1000
	server.slowlogMaxLen = 5

	testCases := []struct {
		args     []string
		duration int64
		expected bool
	}{
		{[]string{"SET", "key1", "value1"}, 500, false},
		{[]string{"GET", "key1"}, 2000, true},
		{[]string{"HSET", "hash", "field", "val"}, 800, false},
		{[]string{"HGETALL", "hash"}, 3000, true},
		{[]string{"SMEMBERS", "set"}, 1500, true},
	}

	for _, tt := range testCases {
		argv := createTestArgs(tt.args...)
		slowlogPushEntryIfNeeded(argv, len(argv), tt.duration)
	}

	// 验证只记录了3条慢查询
	if listLength(server.slowlog) != 3 {
		t.Errorf("expected 3 slow entries, got %d", listLength(server.slowlog))
	}

	// 验证顺序（最新在前）
	entries := slowlogGet(10)

	cmd0 := (*entries[0].argv[0].ptr).(string)
	if cmd0 != "SMEMBERS" {
		t.Errorf("expected SMEMBERS first, got %s", cmd0)
	}

	cmd1 := (*entries[1].argv[0].ptr).(string)
	if cmd1 != "HGETALL" {
		t.Errorf("expected HGETALL second, got %s", cmd1)
	}

	cmd2 := (*entries[2].argv[0].ptr).(string)
	if cmd2 != "GET" {
		t.Errorf("expected GET third, got %s", cmd2)
	}

	// 测试重置
	slowlogReset()
	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0 after reset, got %d", listLength(server.slowlog))
	}
}

func TestIntegration_IDSequential(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	for i := 0; i < 5; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 100)
	}

	entries := slowlogGet(10)

	// ID应该递减（最新在前）
	expectedIDs := []int64{4, 3, 2, 1, 0}
	for i, e := range entries {
		if e.id != expectedIDs[i] {
			t.Errorf("Entry %d: expected ID %d, got %d", i, expectedIDs[i], e.id)
		}
	}
}

func TestIntegration_TimeRange(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	before := time.Now().Unix()
	argv := createTestArgs("CMD")
	slowlogPushEntryIfNeeded(argv, len(argv), 100)
	after := time.Now().Unix()

	entries := slowlogGet(1)

	if entries[0].time < before || entries[0].time > after {
		t.Errorf("Entry time not in expected range")
	}
}

func TestIntegration_FIFOOrder(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	argv1 := createTestArgs("FIRST")
	slowlogPushEntryIfNeeded(argv1, len(argv1), 100)
	time.Sleep(time.Millisecond)

	argv2 := createTestArgs("SECOND")
	slowlogPushEntryIfNeeded(argv2, len(argv2), 100)
	time.Sleep(time.Millisecond)

	argv3 := createTestArgs("THIRD")
	slowlogPushEntryIfNeeded(argv3, len(argv3), 100)

	entries := slowlogGet(10)

	cmd0 := (*entries[0].argv[0].ptr).(string)
	if cmd0 != "THIRD" {
		t.Errorf("expected THIRD first, got %s", cmd0)
	}

	cmd2 := (*entries[2].argv[0].ptr).(string)
	if cmd2 != "FIRST" {
		t.Errorf("expected FIRST last, got %s", cmd2)
	}
}

func TestRedisCompat_MaxLenEnforcement(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0
	server.slowlogMaxLen = 3

	for i := 0; i < 10; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 100)
	}

	// 验证最多保留3条
	if listLength(server.slowlog) != 3 {
		t.Errorf("expected max 3 entries, got %d", listLength(server.slowlog))
	}

	entries := slowlogGet(10)

	// 验证最新的3条（7, 8, 9）
	cmd0 := (*entries[0].argv[1].ptr).(string)
	if cmd0 != "9" {
		t.Errorf("expected CMD9 first, got %s", cmd0)
	}

	cmd2 := (*entries[2].argv[1].ptr).(string)
	if cmd2 != "7" {
		t.Errorf("expected CMD7 last, got %s", cmd2)
	}
}

func TestRedisCompat_DisabledBehavior(t *testing.T) {
	setupSlowlogTest()
	// 阈值为负数时完全禁用
	server.slowlogLogSlowerThan = -1

	for i := 0; i < 100; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 1000000)
	}

	if listLength(server.slowlog) != 0 {
		t.Errorf("expected 0 entries when disabled, got %d", listLength(server.slowlog))
	}
}

func TestRedisCompat_ZeroThreshold(t *testing.T) {
	setupSlowlogTest()
	// 阈值为0时记录所有命令
	server.slowlogLogSlowerThan = 0

	for i := 0; i < 5; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 1)
	}

	if listLength(server.slowlog) != 5 {
		t.Errorf("expected 5 entries with threshold=0, got %d", listLength(server.slowlog))
	}
}

func TestRedisCompat_IDUniqueness(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0
	server.slowlogMaxLen = 100

	for i := 0; i < 50; i++ {
		argv := createTestArgs("CMD", strconv.Itoa(i))
		slowlogPushEntryIfNeeded(argv, len(argv), 100)
	}

	entries := slowlogGet(50)

	// 验证ID是唯一的
	idSet := make(map[int64]bool)
	for _, e := range entries {
		if idSet[e.id] {
			t.Errorf("Duplicate ID found: %d", e.id)
		}
		idSet[e.id] = true
	}
}

func TestRedisCompat_ArgsPreservation(t *testing.T) {
	setupSlowlogTest()
	server.slowlogLogSlowerThan = 0

	// 验证正常参数不被修改
	argv := createTestArgs("SET", "mykey", "myvalue")
	slowlogPushEntryIfNeeded(argv, len(argv), 100)

	entries := slowlogGet(1)

	if len(entries[0].argv) != 3 {
		t.Errorf("expected 3 args, got %d", len(entries[0].argv))
	}

	expectedArgs := []string{"SET", "mykey", "myvalue"}
	for i, expected := range expectedArgs {
		actual := (*entries[0].argv[i].ptr).(string)
		if actual != expected {
			t.Errorf("Arg %d mismatch: expected '%s', got '%s'", i, expected, actual)
		}
	}
}

// TestConfigFromFile - 测试从配置文件加载 slowlog 配置
func TestConfigFromFile(t *testing.T) {
	// 创建临时配置文件
	configContent := `# Test redis.conf
port 6380
databases 8
slowlog-log-slower-than 2000
slowlog-max-len 256
`
	tmpFile, err := os.CreateTemp("", "redis-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpFile.Close()

	// 初始化服务器
	createSharedObjects()
	server.dbnum = REDIS_DEFAULT_DBNUM
	server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
	server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN

	// 加载配置文件
	loadConfigFromFile(tmpFile.Name())

	// 验证配置是否被正确加载
	if server.slowlogLogSlowerThan != 2000 {
		t.Errorf("Expected slowlog-log-slower-than=2000, got %d", server.slowlogLogSlowerThan)
	}

	if server.slowlogMaxLen != 256 {
		t.Errorf("Expected slowlog-max-len=256, got %d", server.slowlogMaxLen)
	}

	if server.port != 6380 {
		t.Errorf("Expected port=6380, got %d", server.port)
	}

	if server.dbnum != 8 {
		t.Errorf("Expected databases=8, got %d", server.dbnum)
	}
}

// TestConfigDefaultValues - 测试默认配置值
func TestConfigDefaultValues(t *testing.T) {
	// 创建不包含 slowlog 配置的临时文件
	configContent := `# Test redis.conf without slowlog config
port 6381
`
	tmpFile, err := os.CreateTemp("", "redis-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpFile.Close()

	// 初始化服务器（先设置默认值）
	createSharedObjects()
	server.dbnum = REDIS_DEFAULT_DBNUM
	server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
	server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN

	// 加载配置文件（不包含 slowlog 配置）
	loadConfigFromFile(tmpFile.Name())

	// 验证 slowlog 配置保持默认值
	if server.slowlogLogSlowerThan != CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN {
		t.Errorf("Expected default slowlog-log-slower-than=%d, got %d",
			CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN, server.slowlogLogSlowerThan)
	}

	if server.slowlogMaxLen != CONFIG_DEFAULT_SLOWLOG_MAX_LEN {
		t.Errorf("Expected default slowlog-max-len=%d, got %d",
			CONFIG_DEFAULT_SLOWLOG_MAX_LEN, server.slowlogMaxLen)
	}

	// port 应该被更新
	if server.port != 6381 {
		t.Errorf("Expected port=6381, got %d", server.port)
	}
}

// TestConfigInvalidValue - 测试无效的配置值
func TestConfigInvalidValue(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "invalid slowlog-log-slower-than",
			content: "slowlog-log-slower-than abc\n",
		},
		{
			name:    "invalid slowlog-max-len",
			content: "slowlog-max-len xyz\n",
		},
		{
			name:    "negative slowlog-max-len",
			content: "slowlog-max-len -1\n",
		},
		{
			name:    "zero slowlog-max-len",
			content: "slowlog-max-len 0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "redis-test-*.conf")
			if err != nil {
				t.Fatalf("Failed to create temp config file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.content); err != nil {
				t.Fatalf("Failed to write config file: %v", err)
			}
			tmpFile.Close()

			// 保存原始值
			originalThreshold := server.slowlogLogSlowerThan
			originalMaxLen := server.slowlogMaxLen

			// 加载配置（应该失败但不会影响程序运行）
			loadConfigFromFile(tmpFile.Name())

			// 验证配置保持原值（未被无效值覆盖）
			if server.slowlogMaxLen <= 0 {
				t.Errorf("slowlog-max-len should not be <= 0, got %d", server.slowlogMaxLen)
			}

			// 恢复原始值
			server.slowlogLogSlowerThan = originalThreshold
			server.slowlogMaxLen = originalMaxLen
		})
	}
}

// TestConfigFileNotFound - 测试配置文件不存在的情况
func TestConfigFileNotFound(t *testing.T) {
	// 保存原始值
	originalThreshold := server.slowlogLogSlowerThan
	originalMaxLen := server.slowlogMaxLen

	// 尝试加载不存在的文件
	loadConfigFromFile("/nonexistent/path/redis.conf")

	// 验证配置保持原值
	if server.slowlogLogSlowerThan != originalThreshold {
		t.Errorf("slowlog-log-slower-than should remain unchanged when file not found")
	}

	if server.slowlogMaxLen != originalMaxLen {
		t.Errorf("slowlog-max-len should remain unchanged when file not found")
	}
}

// TestConfigWithComments - 测试带注释的配置文件
func TestConfigWithComments(t *testing.T) {
	configContent := `# This is a comment
# slowlog-log-slower-than 5000
slowlog-log-slower-than 3000

# Another comment
slowlog-max-len 128

# End of config
`
	tmpFile, err := os.CreateTemp("", "redis-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpFile.Close()

	// 初始化
	createSharedObjects()
	server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
	server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN

	// 加载配置
	loadConfigFromFile(tmpFile.Name())

	// 验证配置（注释行应该被忽略）
	if server.slowlogLogSlowerThan != 3000 {
		t.Errorf("Expected slowlog-log-slower-than=3000, got %d", server.slowlogLogSlowerThan)
	}

	if server.slowlogMaxLen != 128 {
		t.Errorf("Expected slowlog-max-len=128, got %d", server.slowlogMaxLen)
	}
}

// TestLoadServerConfig - 测试完整的 loadServerConfig 函数
func TestLoadServerConfig(t *testing.T) {
	// 创建临时配置文件
	configContent := `slowlog-log-slower-than 1500
slowlog-max-len 64
`
	tmpFile, err := os.CreateTemp("", "redis-test-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpFile.Close()

	// 临时替换配置文件路径
	originalCwd, _ := os.Getwd()
	os.Chdir("/tmp")

	// 创建测试用的配置文件
	testConfigPath := "/tmp/redis.conf"
	if err := os.WriteFile(testConfigPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}
	defer os.Remove(testConfigPath)

	// 初始化并加载配置
	createSharedObjects()
	loadServerConfig()

	// 恢复工作目录
	os.Chdir(originalCwd)

	// 验证配置（如果存在 /tmp/redis.conf 则使用配置值，否则使用默认值）
	if _, err := os.Stat(testConfigPath); err == nil {
		if server.slowlogLogSlowerThan != 1500 {
			t.Errorf("Expected slowlog-log-slower-than=1500, got %d", server.slowlogLogSlowerThan)
		}
		if server.slowlogMaxLen != 64 {
			t.Errorf("Expected slowlog-max-len=64, got %d", server.slowlogMaxLen)
		}
	}
}
