# Redis 功能移植规则

## 命名规范
- 函数/变量名与 Redis C 源码保持 camelCase 一致（如 slowlogPushEntryIfNeeded）
- 常量用 UPPER_SNAKE_CASE，与 Redis 宏定义对应（如 SLOWLOG_ENTRY_MAX_ARGC）
- Go 结构体对应 Redis 的 typedef struct（如 slowlogEntry）

## Redis C 源码到 Go 的映射
- `list *` → `*list`（复用 adlist.go）
- `robj **argv` → `[]*robj`
- `long long` → `int64`
- `unsigned long` → `int64`
- `time_t` → `int64`（Unix 时间戳）
- `ustime()` → `time.Now().UnixMicro()`
- `time(NULL)` → `time.Now().Unix()`
- `listAddNodeHead` → 直接使用（adlist.go 已实现）
- `listDelNode` → 直接使用

## 命令处理函数模板
```go
// xxxCommand - 处理 XXX 命令
func xxxCommand(c *redisClient) {
    // 1. 参数校验
    if c.argc < N {
        errMsg := "wrong number of arguments for 'xxx' command"
        addReplyError(c, &errMsg)
        return
    }

    // 2. 子命令分发（如有）
    subCmd := (*c.argv[1].ptr).(string)
    switch strings.ToUpper(subCmd) {
    case "GET":
        // ...
    default:
        errMsg := "unknown subcommand '" + subCmd + "'"
        addReplyError(c, &errMsg)
    }

    // 3. 参数提取
    s := (*c.argv[i].ptr).(string)
    n, err := strconv.ParseInt(s, 10, 64)

    // 4. 响应
    addReplyMultiBulkLen(c, length)
    addReplyBulk(c, obj)
    addReplyLongLong(c, value)
    addReply(c, shared.ok)
}
```

## 命令注册
在 command.go 的 redisCommandTable 中添加：
```go
{name: "XXX", proc: xxxCommand, arity: N, sflag: "wmF", flag: 0}
```
- arity: 正数=精确参数数，负数=至少 abs(arity) 个参数，0=不限
- sflag: w=写, r=读, m=内存敏感, F=快速, a=管理, t=从库可用

## Redis 行为兼容性要求
- 负数阈值 = 禁用功能（不记录、不执行）
- 零阈值 = 启用全部（记录所有、无限制）
- FIFO 队列：新数据加 head，淘汰 tail
- ID/计数器重置后继续递增，不归零
- 错误消息与 Redis 原文一致：
  - "wrong number of arguments for 'xxx' command"
  - "value is not an integer or out of range"
  - "unknown subcommand"
  - "no such key"

## 参数截断规则
- 参数数量超过阈值时：保留前 N-1 个 + "... (X more arguments)"
- 字符串超过阈值时：保留前 N 字节 + "... (X more bytes)"

## call() 集成点
在 redis.go 的 call() 函数中集成时间测量：
```go
func call(c *redisClient, flags int) {
    start := time.Now().UnixMicro()
    c.cmd.proc(c)
    duration := time.Now().UnixMicro() - start
    if flags&REDIS_CALL_SLOWLOG != 0 {
        slowlogPushEntryIfNeeded(c.argv, c.argc, duration)
    }
}
```