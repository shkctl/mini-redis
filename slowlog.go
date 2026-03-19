package main

import (
	"strconv"
	"strings"
	"time"
)

// slowlogEntry - 慢查询条目
// 参考Redis: slowlog.h中的slowlogEntry结构
type slowlogEntry struct {
	id       int64   // 唯一递增ID，参考: se->id
	time     int64   // Unix时间戳（秒），参考: se->time = time(NULL)
	duration int64   // 执行时长（微秒），参考: se->duration
	argv     []*robj // 命令参数，复用现有robj类型
	argc     int     // 参数个数
}

// slowlogInit - 初始化慢查询日志
// 参考Redis: slowlogInit()
func slowlogInit() {
	server.slowlog = listCreate()
	server.slowlogEntryId = 0
}

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

// slowlogReset - 清空慢查询日志
// 参考Redis: slowlogReset()
//
//	void slowlogReset(void) {
//	    while (listLength(server.slowlog) > 0)
//	        listDelNode(server.slowlog, listLast(server.slowlog));
//	}
func slowlogReset() {
	for listLength(server.slowlog) > 0 {
		listDelNode(server.slowlog, server.slowlog.tail)
	}
}

// slowlogSetMaxLen - 设置最大记录数并清理超出的记录
// 参考Redis: CONFIG SET slowlog-max-len
func slowlogSetMaxLen(maxLen int64) {
	server.slowlogMaxLen = maxLen

	// 清理超出新限制的记录
	for listLength(server.slowlog) > server.slowlogMaxLen {
		listDelNode(server.slowlog, server.slowlog.tail)
	}
}

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
