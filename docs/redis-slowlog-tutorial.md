# mini-redis慢查询功能实现 - TDD开发教程

## 目录

1. [项目概述](#项目概述)
2. [Redis慢查询功能分析](#redis慢查询功能分析)
3. [核心数据结构实现](#核心数据结构实现)
4. [慢查询记录功能](#慢查询记录功能)
5. [查询功能](#查询功能)
6. [重置功能](#重置功能)
7. [配置功能](#配置功能)
8. [命令集成](#命令集成)
9. [集成测试](#集成测试)
10. [Redis行为对比验证](#redis行为对比验证)
11. [完整代码参考](#完整代码参考)

---

## 项目概述

### 1.1 目标

通过TDD（测试驱动开发）方法，为mini-redis项目实现慢查询（SLOWLOG）功能。本教程参考Redis C语言源代码的实现模式，采用与现有代码一致的面向过程编程风格，复用项目现有的双向链表（adlist.go）等基础设施。

### 1.2 Redis慢查询功能简介

Redis慢查询功能的核心目标是**记录执行时间超过指定阈值的命令**，帮助开发者识别和优化性能问题。

**核心设计思想**：
> "记住最近N条执行时间超过M微秒的命令"

**关键技术特点**：
- 时间精度：微秒级（μs）
- 存储方式：内存链表（FIFO队列）
- 被动收集：命令执行完成后才记录
- 零外部依赖：通过SLOWLOG命令查询

---

## Redis慢查询功能分析

### 2.1 Redis源代码文件结构

| 文件 | 作用 |
|------|------|
| `src/slowlog.h` | 慢查询数据结构定义 |
| `src/slowlog.c` | 慢查询核心实现 |
| `src/server.h` | 服务器配置常量定义 |
| `src/server.c` | 命令调用和时间测量 |

### 2.2 核心数据结构（Redis C语言）

```c
// slowlog.h
typedef struct slowlogEntry {
    robj **argv;              // 命令参数数组
    int argc;                // 参数个数
    long long id;            // 唯一递增ID
    long long duration;       // 执行时长（微秒）
    time_t time;              // Unix时间戳
} slowlogEntry;

// server.h - 服务器状态中的配置
list *slowlog;                        // 慢查询链表
long long slowlog_entry_id;           // 全局递增ID
long long slowlog_log_slower_than;    // 阈值（微秒）
unsigned long slowlog_max_len;        // 最大记录数
```

### 2.3 配置常量

```c
#define CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN 10000  // 10毫秒
#define CONFIG_DEFAULT_SLOWLOG_MAX_LEN 128            // 最多128条
#define SLOWLOG_ENTRY_MAX_ARGC 32                     // 最大参数数量
#define SLOWLOG_ENTRY_MAX_STRING 128                  // 最大参数字符串长度
```

### 2.4 命令执行与时间测量流程

```
客户端发送命令
    ↓
processCommand() - 命令解析和验证
    ↓
call(c, CMD_CALL_FULL) - 执行命令
    ↓
┌──────────────────────────────────────┐
│ 1. start = ustime()                 │
│ 2. c->cmd->proc(c)                  │
│ 3. duration = ustime() - start      │
│ 4. slowlogPushEntryIfNeeded()       │
└──────────────────────────────────────┘
```

### 2.5 关键实现细节

#### 链表操作
- 新记录添加到链表**头部**（`listAddNodeHead`）
- 最旧记录从链表**尾部**删除（`listDelNode`）
- 使用引用计数管理参数对象内存

#### 参数截断规则
- 参数数量超过32个：保留前31个 + 省略信息
- 参数字符串超过128字节：保留前128字节 + 省略信息

#### 配置管理
- `slowlog-log-slower-than`：阈值设置，负数禁用，0记录所有命令
- `slowlog-max-len`：最大记录数限制

---

## 核心数据结构实现

### 3.1 Step 1 - 扩展redisServer结构

**文件**: `redis.go`

在现有`redisServer`结构体中添加慢查询相关字段：

```go
// redis.go - 在现有常量区域添加
// 注意：REDIS_CALL_SLOWLOG 等调用标志常量已在 redis.go 中定义（第27-32行），无需重复添加
const (
    // Slowlog相关常量
    SLOWLOG_ENTRY_MAX_ARGC   = 32    // 最大参数数量
    SLOWLOG_ENTRY_MAX_STRING = 128   // 最大参数字符串长度
    CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN = 10000  // 默认阈值10毫秒
    CONFIG_DEFAULT_SLOWLOG_MAX_LEN = 128            // 默认最多128条
)

// redis.go - 在redisServer结构体中添加字段
type redisServer struct {
    // ... 现有字段保持不变
    
    // Slowlog相关字段
    slowlog              *list   // 复用现有adlist，参考: list *slowlog
    slowlogEntryId       int64   // 全局递增ID，参考: slowlog_entry_id
    slowlogLogSlowerThan int64   // 阈值（微秒），参考: slowlog_log_slower_than
    slowlogMaxLen        int64   // 最大记录数，参考: slowlog_max_len
}
```

### 3.2 Step 2 - 定义slowlogEntry结构

**文件**: `slowlog.go`（新建）

```go
package main

import (
    "time"
)

// slowlogEntry - 慢查询条目
// 参考Redis: slowlog.h中的slowlogEntry结构
type slowlogEntry struct {
    id       int64     // 唯一递增ID，参考: se->id
    time     int64     // Unix时间戳（秒），参考: se->time = time(NULL)
    duration int64     // 执行时长（微秒），参考: se->duration
    argv     []*robj   // 命令参数，复用现有robj类型
    argc     int       // 参数个数
}
```

### 3.3 Step 3 - 测试初始化

**文件**: `slowlog_test.go`（新建）

```go
package main

import (
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
```

### 3.4 Step 4 - 运行测试（预期失败）

```bash
cd ~/github/mini-redis
go test -v -run TestSlowlogInit
```

### 3.5 Step 5 - 实现初始化函数

**文件**: `slowlog.go`（添加初始化函数）

```go
// slowlogInit - 初始化慢查询日志
// 参考Redis: slowlogInit()
func slowlogInit() {
    server.slowlog = listCreate()
    server.slowlogEntryId = 0
}
```

**文件**: `redis.go`（在initServer中调用）

```go
func initServer() {
    // ... 现有初始化代码
    
    // 初始化slowlog
    slowlogInit()
}
```

### 3.6 Step 6 - 实现默认配置加载

**文件**: `redis.go`（在loadServerConfig中添加）

```go
func loadServerConfig() {
    log.Println("load redis server config")
    server.dbnum = REDIS_DEFAULT_DBNUM
    
    // Slowlog默认配置
    server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
    server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN
}
```

### 3.7 Step 7 - 再次运行测试

```bash
go test -v -run TestSlowlogInit
```

**预期输出**:
```
=== RUN   TestSlowlogInit
--- PASS: TestSlowlogInit (0.00s)
```

---

## 慢查询记录功能

### 4.1 Step 8 - 测试slowlogPushEntryIfNeeded基本功能

**文件**: `slowlog_test.go`（添加测试）

```go
// 测试辅助函数：创建测试用的robj参数
func createTestArgs(args ...string) []*robj {
    result := make([]*robj, len(args))
    for i, arg := range args {
        s := arg
        result[i] = createStringObject(&s, len(s))
    }
    return result
}

func TestSlowlogPushEntry_Basic(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 1000  // 阈值1000微秒
    
    argv := createTestArgs("GET", "mykey")
    duration := int64(1500)  // 超过阈值
    
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
```

### 4.2 Step 9 - 运行测试（预期失败）

```bash
go test -v -run TestSlowlogPushEntry_Basic
```

### 4.3 Step 10 - 实现slowlogPushEntryIfNeeded

**文件**: `slowlog.go`（添加核心函数）

```go
import (
    "strconv"
    "time"
)

// slowlogCreateEntry - 创建慢查询条目
// 参考Redis: slowlogCreateEntry()
func slowlogCreateEntry(argv []*robj, argc int, duration int64) *slowlogEntry {
    se := &slowlogEntry{
        id:       server.slowlogEntryId,
        time:     time.Now().Unix(),
        duration: duration,
        argc:     argc,
    }
    server.slowlogEntryId++
    
    // 处理参数截断
    slargc := argc
    if slargc > SLOWLOG_ENTRY_MAX_ARGC {
        slargc = SLOWLOG_ENTRY_MAX_ARGC
    }
    
    se.argv = make([]*robj, 0, slargc)
    
    for j := 0; j < slargc; j++ {
        // 最后一个位置用于显示省略信息
        if slargc != argc && j == slargc-1 {
            remaining := argc - slargc + 1
            omitMsg := "... (" + strconv.Itoa(remaining) + " more arguments)"
            se.argv = append(se.argv, createStringObject(&omitMsg, len(omitMsg)))
        } else {
            // 处理字符串类型
            if argv[j].encoding == REDIS_ENCODING_EMBSTR {
                s := (*argv[j].ptr).(string)
                if len(s) > SLOWLOG_ENTRY_MAX_STRING {
                    moreBytes := len(s) - SLOWLOG_ENTRY_MAX_STRING
                    truncated := s[:SLOWLOG_ENTRY_MAX_STRING] + "... (" + strconv.Itoa(moreBytes) + " more bytes)"
                    se.argv = append(se.argv, createStringObject(&truncated, len(truncated)))
                } else {
                    se.argv = append(se.argv, createStringObject(&s, len(s)))
                }
            } else if argv[j].encoding == REDIS_ENCODING_INT {
                // 处理整数类型：转为字符串
                num := (*argv[j].ptr).(int64)
                s := strconv.FormatInt(num, 10)
                se.argv = append(se.argv, createStringObject(&s, len(s)))
            } else {
                // 其他类型直接复制
                se.argv = append(se.argv, argv[j])
            }
        }
    }
    
    return se
}

// slowlogPushEntryIfNeeded - 如果命令执行时间超过阈值，则记录到慢查询日志
// 参考Redis: slowlogPushEntryIfNeeded()
func slowlogPushEntryIfNeeded(argv []*robj, argc int, duration int64) {
    // 阈值为负数时禁用慢查询
    if server.slowlogLogSlowerThan < 0 {
        return
    }
    
    // 检查是否超过阈值
    if duration >= server.slowlogLogSlowerThan {
        // 创建条目并添加到链表头部
        entry := slowlogCreateEntry(argv, argc, duration)
        value := interface{}(entry)
        listAddNodeHead(server.slowlog, &value)
        
        // 保持FIFO，清理超出限制的旧记录
        for listLength(server.slowlog) > server.slowlogMaxLen {
            listDelNode(server.slowlog, server.slowlog.tail)
        }
    }
}
```

### 4.4 Step 11 - 再次运行测试

```bash
go test -v -run TestSlowlogPushEntry_Basic
```

**预期输出**:
```
=== RUN   TestSlowlogPushEntry_Basic
--- PASS: TestSlowlogPushEntry_Basic (0.00s)
```

---

## 参数截断功能

### 4.5 Step 12 - 测试参数数量截断

**文件**: `slowlog_test.go`（添加测试）

```go
func TestSlowlogPushEntry_ArgcLimit(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0  // 记录所有命令
    
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
```

### 4.6 Step 13 - 运行测试验证截断功能

```bash
go test -v -run TestSlowlogPushEntry_ArgcLimit
```

### 4.7 Step 14 - 测试禁用功能

```go
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
```

### 4.8 Step 15 - 测试阈值过滤

```go
func TestSlowlogPushEntry_Threshold(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 1000
    
    // 快速命令（500 < 1000）不应记录
    argv1 := createTestArgs("FAST")
    slowlogPushEntryIfNeeded(argv1, 500)
    if listLength(server.slowlog) != 0 {
        t.Errorf("expected 0 entries for fast command, got %d", listLength(server.slowlog))
    }
    
    // 慢速命令（1500 > 1000）应该记录
    argv2 := createTestArgs("SLOW")
    slowlogPushEntryIfNeeded(argv2, 1500)
    if listLength(server.slowlog) != 1 {
        t.Errorf("expected 1 entry for slow command, got %d", listLength(server.slowlog))
    }
}
```

### 4.9 Step 16 - 测试阈值=0（记录所有命令）

```go
func TestSlowlogPushEntry_ThresholdZero(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0
    
    argv1 := createTestArgs("CMD1")
    argv2 := createTestArgs("CMD2")
    slowlogPushEntryIfNeeded(argv1, 100)
    slowlogPushEntryIfNeeded(argv2, 500)
    
    if listLength(server.slowlog) != 2 {
        t.Errorf("expected 2 entries with threshold=0, got %d", listLength(server.slowlog))
    }
}
```

### 4.10 Step 17 - 运行所有PushEntry测试

```bash
go test -v -run TestSlowlogPushEntry
```

---

## 查询功能

### 5.1 Step 18 - 测试slowlogLen

**文件**: `slowlog_test.go`（添加测试）

```go
func TestSlowlogLen(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0
    
    // 空时为0
    if listLength(server.slowlog) != 0 {
        t.Errorf("expected 0, got %d", listLength(server.slowlog))
    }
    
    // 添加一条记录
    argv1 := createTestArgs("CMD1")
    slowlogPushEntryIfNeeded(argv1, 100)
    if listLength(server.slowlog) != 1 {
        t.Errorf("expected 1, got %d", listLength(server.slowlog))
    }
    
    // 添加第二条记录
    argv2 := createTestArgs("CMD2")
    slowlogPushEntryIfNeeded(argv2, 100)
    if listLength(server.slowlog) != 2 {
        t.Errorf("expected 2, got %d", listLength(server.slowlog))
    }
}
```

### 5.2 Step 19 - 说明

**注意**: `slowlogLen`直接使用现有的`listLength(server.slowlog)`，无需额外实现。

---

### 5.3 Step 20 - 测试slowlogGet

```go
func TestSlowlogGet(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0
    
    // 添加3条记录
    argv1 := createTestArgs("CMD1", "arg1")
    argv2 := createTestArgs("CMD2", "arg2")
    argv3 := createTestArgs("CMD3", "arg3")
    slowlogPushEntryIfNeeded(argv1, 100)
    slowlogPushEntryIfNeeded(argv2, 200)
    slowlogPushEntryIfNeeded(argv3, 300)
    
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
```

### 5.4 Step 21 - 实现slowlogGet

**文件**: `slowlog.go`（添加函数）

```go
// slowlogGet - 获取慢查询条目
// count: 要获取的条目数量，<=0时使用默认值10
// 参考Redis: slowlogCommand() -> SLOWLOG GET实现
func slowlogGet(count int64) []*slowlogEntry {
    // 使用默认值10
    if count <= 0 {
        count = 10
    }
    
    // 不能超过实际条目数
    length := listLength(server.slowlog)
    if count > length {
        count = length
    }
    
    result := make([]*slowlogEntry, 0, count)
    node := listFirst(server.slowlog)
    
    for i := int64(0); i < count && node != nil; i++ {
        entry := (*node.value).(*slowlogEntry)
        result = append(result, entry)
        node = node.next
    }
    
    return result
}
```

### 5.5 Step 22 - 运行测试

```bash
go test -v -run TestSlowlogGet
```

---

## 重置功能

### 6.1 Step 23 - 测试slowlogReset

**文件**: `slowlog_test.go`（添加测试）

```go
func TestSlowlogReset(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0
    
    // 添加记录
    argv1 := createTestArgs("CMD1")
    argv2 := createTestArgs("CMD2")
    slowlogPushEntryIfNeeded(argv1, 100)
    slowlogPushEntryIfNeeded(argv2, 100)
    
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
    slowlogPushEntryIfNeeded(argv1, 100)
    slowlogReset()
    
    // 重置后ID应该继续递增（与Redis行为一致）
    argv2 := createTestArgs("CMD2")
    slowlogPushEntryIfNeeded(argv2, 100)
    
    entries := slowlogGet(1)
    // ID应该是1，因为之前已经用了0
    if entries[0].id != 1 {
        t.Errorf("expected ID 1 after reset, got %d", entries[0].id)
    }
}
```

### 6.2 Step 24 - 实现slowlogReset

**文件**: `slowlog.go`（添加函数）

```go
// slowlogReset - 清空慢查询日志
// 参考Redis: slowlogReset()
// void slowlogReset(void) {
//     while (listLength(server.slowlog) > 0)
//         listDelNode(server.slowlog, listLast(server.slowlog));
// }
func slowlogReset() {
    for listLength(server.slowlog) > 0 {
        listDelNode(server.slowlog, server.slowlog.tail)
    }
}
```

### 6.3 Step 25 - 运行测试

```bash
go test -v -run TestSlowlogReset
```

---

## 配置功能

### 7.1 Step 26 - 测试配置功能

**文件**: `slowlog_test.go`（添加测试）

```go
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
    slowlogPushEntryIfNeeded(argv1, 2000)
    if listLength(server.slowlog) != 1 {
        t.Errorf("expected 1 slow entry, got %d", listLength(server.slowlog))
    }
    
    // 增加阈值到3000
    server.slowlogLogSlowerThan = 3000
    
    // 2500 < 3000，不应该记录
    argv2 := createTestArgs("NOT_SLOW")
    slowlogPushEntryIfNeeded(argv2, 2500)
    if listLength(server.slowlog) != 1 {
        t.Errorf("expected still 1 after threshold change, got %d", listLength(server.slowlog))
    }
}
```

### 7.2 Step 27 - 实现配置方法

**文件**: `slowlog.go`（添加函数）

```go
// slowlogSetMaxLen - 设置最大记录数并清理超出的记录
// 参考Redis: CONFIG SET slowlog-max-len
func slowlogSetMaxLen(maxLen int64) {
    server.slowlogMaxLen = maxLen
    
    // 清理超出新限制的记录
    for listLength(server.slowlog) > server.slowlogMaxLen {
        listDelNode(server.slowlog, server.slowlog.tail)
    }
}
```

### 7.3 Step 28 - 运行测试

```bash
go test -v -run TestSlowlogSet
```

---

## 命令集成

### 8.1 Step 29 - 在call()中集成时间测量

**文件**: `redis.go`（修改call函数）

```go
import "time"

// call - 执行命令并记录慢查询
// 参考Redis: call(client *c, int flags)
func call(c *redisClient, flags int) {
    // 记录开始时间（微秒）
    start := time.Now().UnixMicro()
    
    // 执行命令
    c.cmd.proc(c)
    
    // 计算执行时长
    duration := time.Now().UnixMicro() - start
    
    // 记录慢查询
    if flags&REDIS_CALL_SLOWLOG != 0 {
        slowlogPushEntryIfNeeded(c.argv, c.argc, duration)
    }
}
```

### 8.2 Step 30 - 注册SLOWLOG命令

**文件**: `command.go`（在redisCommandTable中添加）

```go
var redisCommandTable = []redisCommand{
    // ... 现有命令保持不变
    // arity: -2 表示至少2个参数（SLOWLOG + 子命令），参数数量不固定
    {name: "SLOWLOG", proc: slowlogCommand, arity: -2, sflag: "a", flag: 0},
}
```

### 8.3 Step 31 - 实现slowlogCommand

**文件**: `slowlog.go`（添加命令处理函数，需在import中添加"strings"）

```go
import (
    "strconv"
    "strings"  // 用于strings.ToUpper()
    "time"
)
// slowlogCommand - 处理SLOWLOG命令
// 参考Redis: slowlogCommand()
// 支持子命令: GET [count], LEN, RESET
func slowlogCommand(c *redisClient) {
    if c.argc < 2 {
        errMsg := "wrong number of arguments for 'slowlog' command"
        addReplyError(c, &errMsg)
        return
    }
    
    subCmd := (*c.argv[1].ptr).(string)
    
    switch strings.ToUpper(subCmd) {
    case "GET":
        slowlogCommandGet(c)
    case "LEN":
        slowlogCommandLen(c)
    case "RESET":
        slowlogCommandReset(c)
    default:
        errMsg := "unknown subcommand '" + subCmd + "'. Try SLOWLOG GET, SLOWLOG LEN, SLOWLOG RESET."
        addReplyError(c, &errMsg)
    }
}

// slowlogCommandGet - 处理SLOWLOG GET [count]
func slowlogCommandGet(c *redisClient) {
    var count int64 = 10
    
    // c.argc 包含命令本身，SLOWLOG GET [count] 应该是 2 或 3 个参数
    // c.argv[0] = "SLOWLOG", c.argv[1] = "GET", c.argv[2] = count (可选)
    if c.argc > 3 {
        errMsg := "wrong number of arguments for 'slowlog' command"
        addReplyError(c, &errMsg)
        return
    }
    
    if c.argc == 3 {
        s := (*c.argv[2].ptr).(string)
        n, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            errMsg := "value is not an integer or out of range"
            addReplyError(c, &errMsg)
            return
        }
        count = n
    }
    
    entries := slowlogGet(count)
    
    // 返回数组格式
    addReplyMultiBulkLen(c, int64(len(entries)))
    
    for _, entry := range entries {
        // 每个条目有4个元素: id, timestamp, duration, argv
        addReplyMultiBulkLen(c, 4)
        addReplyLongLong(c, entry.id)
        addReplyLongLong(c, entry.time)
        addReplyLongLong(c, entry.duration)
        
        // 返回命令参数
        addReplyMultiBulkLen(c, int64(len(entry.argv)))
        for _, arg := range entry.argv {
            addReplyBulk(c, arg)
        }
    }
}

// slowlogCommandLen - 处理SLOWLOG LEN
func slowlogCommandLen(c *redisClient) {
    addReplyLongLong(c, listLength(server.slowlog))
}

// slowlogCommandReset - 处理SLOWLOG RESET
func slowlogCommandReset(c *redisClient) {
    slowlogReset()
    addReply(c, shared.ok)
}
```

### 8.4 Step 32 - 测试命令集成

```go
func TestSlowlogCommand_Get(t *testing.T) {
    setupSlowlogTest()
    server.slowlogLogSlowerThan = 0
    
    // 添加测试数据
    argv := createTestArgs("SET", "key", "value")
    slowlogPushEntryIfNeeded(argv, len(argv), 1500)
    
    // 验证数据已记录
    if listLength(server.slowlog) != 1 {
        t.Errorf("expected 1 entry, got %d", listLength(server.slowlog))
    }
}
```

---

## 集成测试

### 9.1 Step 33 - 综合测试

**文件**: `slowlog_test.go`（添加集成测试）

```go
import (
    "testing"
    "time"
)

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
    slowlogPushEntryIfNeeded(argv1, 100)
    time.Sleep(time.Millisecond)
    
    argv2 := createTestArgs("SECOND")
    slowlogPushEntryIfNeeded(argv2, 100)
    time.Sleep(time.Millisecond)
    
    argv3 := createTestArgs("THIRD")
    slowlogPushEntryIfNeeded(argv3, 100)
    
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
```

### 9.2 Step 34 - 运行集成测试

```bash
go test -v -run TestIntegration
```

---

## Redis行为对比验证

### 10.1 Step 35 - Redis兼容性测试

**文件**: `slowlog_test.go`（添加兼容性测试）

```go
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
```

### 10.2 Step 36 - 运行兼容性测试

```bash
go test -v -run TestRedisCompat
```

---

## 完整代码参考

### A.1 redis.go - 新增常量和字段

```go
// 需要在import中添加"time"
import (
    // ... 现有import
    "time"
)

// 在现有常量区域添加
const (
    // Slowlog相关常量
    SLOWLOG_ENTRY_MAX_ARGC   = 32
    SLOWLOG_ENTRY_MAX_STRING = 128
    CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN = 10000  // 10毫秒
    CONFIG_DEFAULT_SLOWLOG_MAX_LEN = 128
)

// 在redisServer结构体中添加
type redisServer struct {
    // ... 现有字段
    
    // Slowlog相关字段
    slowlog              *list
    slowlogEntryId       int64
    slowlogLogSlowerThan int64
    slowlogMaxLen        int64
}

// 修改call函数
func call(c *redisClient, flags int) {
    start := time.Now().UnixMicro()
    c.cmd.proc(c)
    duration := time.Now().UnixMicro() - start
    
    if flags&REDIS_CALL_SLOWLOG != 0 {
        slowlogPushEntryIfNeeded(c.argv, c.argc, duration)
    }
}

// 在initServer中添加
func initServer() {
    // ... 现有初始化代码
    slowlogInit()
}

// 在loadServerConfig中添加
func loadServerConfig() {
    // ... 现有配置代码
    server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
    server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN
}
```

### A.2 slowlog.go - 完整实现

```go
package main

import (
    "strconv"
    "strings"
    "time"
)

// slowlogEntry - 慢查询条目
type slowlogEntry struct {
    id       int64
    time     int64
    duration int64
    argv     []*robj
    argc     int
}

// slowlogInit - 初始化慢查询日志
func slowlogInit() {
    server.slowlog = listCreate()
    server.slowlogEntryId = 0
}

// slowlogCreateEntry - 创建慢查询条目
func slowlogCreateEntry(argv []*robj, argc int, duration int64) *slowlogEntry {
    se := &slowlogEntry{
        id:       server.slowlogEntryId,
        time:     time.Now().Unix(),
        duration: duration,
        argc:     argc,
    }
    server.slowlogEntryId++
    
    slargc := argc
    if slargc > SLOWLOG_ENTRY_MAX_ARGC {
        slargc = SLOWLOG_ENTRY_MAX_ARGC
    }
    
    se.argv = make([]*robj, 0, slargc)
    
    for j := 0; j < slargc; j++ {
        if slargc != argc && j == slargc-1 {
            remaining := argc - slargc + 1
            omitMsg := "... (" + strconv.Itoa(remaining) + " more arguments)"
            se.argv = append(se.argv, createStringObject(&omitMsg, len(omitMsg)))
        } else {
            // 处理字符串类型
            if argv[j].encoding == REDIS_ENCODING_EMBSTR {
                s := (*argv[j].ptr).(string)
                if len(s) > SLOWLOG_ENTRY_MAX_STRING {
                    moreBytes := len(s) - SLOWLOG_ENTRY_MAX_STRING
                    truncated := s[:SLOWLOG_ENTRY_MAX_STRING] + "... (" + strconv.Itoa(moreBytes) + " more bytes)"
                    se.argv = append(se.argv, createStringObject(&truncated, len(truncated)))
                } else {
                    se.argv = append(se.argv, createStringObject(&s, len(s)))
                }
            } else if argv[j].encoding == REDIS_ENCODING_INT {
                // 处理整数类型：转为字符串
                num := (*argv[j].ptr).(int64)
                s := strconv.FormatInt(num, 10)
                se.argv = append(se.argv, createStringObject(&s, len(s)))
            } else {
                // 其他类型直接复制
                se.argv = append(se.argv, argv[j])
            }
        }
    }
    
    return se
}

// slowlogPushEntryIfNeeded - 记录慢查询
func slowlogPushEntryIfNeeded(argv []*robj, argc int, duration int64) {
    if server.slowlogLogSlowerThan < 0 {
        return
    }
    
    if duration >= server.slowlogLogSlowerThan {
        entry := slowlogCreateEntry(argv, argc, duration)
        value := interface{}(entry)
        listAddNodeHead(server.slowlog, &value)
        
        for listLength(server.slowlog) > server.slowlogMaxLen {
            listDelNode(server.slowlog, server.slowlog.tail)
        }
    }
}

// slowlogGet - 获取慢查询条目
func slowlogGet(count int64) []*slowlogEntry {
    if count <= 0 {
        count = 10
    }
    
    length := listLength(server.slowlog)
    if count > length {
        count = length
    }
    
    result := make([]*slowlogEntry, 0, count)
    node := listFirst(server.slowlog)
    
    for i := int64(0); i < count && node != nil; i++ {
        entry := (*node.value).(*slowlogEntry)
        result = append(result, entry)
        node = node.next
    }
    
    return result
}

// slowlogReset - 清空慢查询日志
func slowlogReset() {
    for listLength(server.slowlog) > 0 {
        listDelNode(server.slowlog, server.slowlog.tail)
    }
}

// slowlogSetMaxLen - 设置最大记录数
func slowlogSetMaxLen(maxLen int64) {
    server.slowlogMaxLen = maxLen
    for listLength(server.slowlog) > server.slowlogMaxLen {
        listDelNode(server.slowlog, server.slowlog.tail)
    }
}

// slowlogCommand - SLOWLOG命令处理
func slowlogCommand(c *redisClient) {
    if c.argc < 2 {
        errMsg := "wrong number of arguments for 'slowlog' command"
        addReplyError(c, &errMsg)
        return
    }
    
    subCmd := (*c.argv[1].ptr).(string)
    
    switch strings.ToUpper(subCmd) {
    case "GET":
        slowlogCommandGet(c)
    case "LEN":
        slowlogCommandLen(c)
    case "RESET":
        slowlogCommandReset(c)
    default:
        errMsg := "unknown subcommand '" + subCmd + "'"
        addReplyError(c, &errMsg)
    }
}

func slowlogCommandGet(c *redisClient) {
    var count int64 = 10
    
    // c.argc 包含命令本身，SLOWLOG GET [count] 应该是 2 或 3 个参数
    // c.argv[0] = "SLOWLOG", c.argv[1] = "GET", c.argv[2] = count (可选)
    if c.argc > 3 {
        errMsg := "wrong number of arguments for 'slowlog' command"
        addReplyError(c, &errMsg)
        return
    }
    
    if c.argc == 3 {
        s := (*c.argv[2].ptr).(string)
        n, err := strconv.ParseInt(s, 10, 64)
        if err != nil {
            errMsg := "value is not an integer or out of range"
            addReplyError(c, &errMsg)
            return
        }
        count = n
    }
    
    entries := slowlogGet(count)
    addReplyMultiBulkLen(c, int64(len(entries)))
    
    for _, entry := range entries {
        addReplyMultiBulkLen(c, 4)
        addReplyLongLong(c, entry.id)
        addReplyLongLong(c, entry.time)
        addReplyLongLong(c, entry.duration)
        
        addReplyMultiBulkLen(c, int64(len(entry.argv)))
        for _, arg := range entry.argv {
            addReplyBulk(c, arg)
        }
    }
}

func slowlogCommandLen(c *redisClient) {
    addReplyLongLong(c, listLength(server.slowlog))
}

func slowlogCommandReset(c *redisClient) {
    slowlogReset()
    addReply(c, shared.ok)
}
```

### A.3 command.go - 命令注册

```go
// 在redisCommandTable中添加
var redisCommandTable = []redisCommand{
    // ... 现有命令
    // arity: -2 表示至少2个参数（SLOWLOG + 子命令），参数数量不固定
    {name: "SLOWLOG", proc: slowlogCommand, arity: -2, sflag: "a", flag: 0},
}
```

---

## 测试运行指南

### 运行所有测试

```bash
cd ~/github/mini-redis
go test -v -run Slowlog
```

### 运行特定测试

```bash
# 初始化测试
go test -v -run TestSlowlogInit

# 记录功能测试
go test -v -run TestSlowlogPushEntry

# 查询功能测试
go test -v -run TestSlowlogGet

# 重置功能测试
go test -v -run TestSlowlogReset

# 配置功能测试
go test -v -run TestSlowlogSet

# 集成测试
go test -v -run TestIntegration

# Redis兼容性测试
go test -v -run TestRedisCompat
```

### 运行带覆盖率的测试

```bash
go test -v -cover -run Slowlog
```

### 生成HTML覆盖率报告

```bash
go test -coverprofile=coverage.out -run Slowlog
go tool cover -html=coverage.out -o coverage.html
```

---

## 总结

本教程通过TDD方法为mini-redis项目实现了慢查询功能，包括：

### 实现内容

| 模块 | 功能 | 文件 |
|------|------|------|
| 数据结构 | `slowlogEntry`结构体 | `slowlog.go` |
| 记录功能 | `slowlogPushEntryIfNeeded` | `slowlog.go` |
| 查询功能 | `slowlogGet` | `slowlog.go` |
| 重置功能 | `slowlogReset` | `slowlog.go` |
| 配置功能 | `slowlogSetMaxLen` | `slowlog.go` |
| 命令集成 | `slowlogCommand` | `slowlog.go` |
| 时间测量 | `call()`函数修改 | `redis.go` |

### 核心设计特点

1. **复用现有基础设施**：使用`adlist.go`双向链表存储慢查询记录
2. **集成式设计**：字段添加到`redisServer`，与项目架构保持一致
3. **复用robj类型**：命令参数使用现有的`*robj`类型
4. **无额外锁**：利用现有channel事件循环保证线程安全
5. **参照Redis源码**：函数命名、逻辑流程与Redis C源码保持一致

### 与Redis行为一致性

- 阈值为负数时禁用慢查询
- 阈值为0时记录所有命令
- 参数截断：最多32个参数，字符串最长128字节
- FIFO队列：新记录在头部，旧记录从尾部删除
- ID全局递增，重置后继续递增

### 文件变更概览

| 文件 | 变更类型 | 主要内容 |
|------|----------|----------|
| `redis.go` | 修改 | 添加常量、字段，修改`call()`和`initServer()` |
| `slowlog.go` | 新建 | 慢查询核心实现 |
| `slowlog_test.go` | 新建 | 单元测试和集成测试 |
| `command.go` | 修改 | 注册SLOWLOG命令 |
