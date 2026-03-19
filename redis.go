package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	REDIS_CMD_WRITE           = 1    /* "w" flag */
	REDIS_CMD_READONLY        = 2    /* "r" flag */
	REDIS_CMD_DENYOOM         = 4    /* "m" flag */
	REDIS_CMD_NOT_USED_1      = 8    /* no longer used flag */
	REDIS_CMD_ADMIN           = 16   /* "a" flag */
	REDIS_CMD_PUBSUB          = 32   /* "p" flag */
	REDIS_CMD_NOSCRIPT        = 64   /* "s" flag */
	REDIS_CMD_RANDOM          = 128  /* "R" flag */
	REDIS_CMD_SORT_FOR_SCRIPT = 256  /* "S" flag */
	REDIS_CMD_LOADING         = 512  /* "l" flag */
	REDIS_CMD_STALE           = 1024 /* "t" flag */
	REDIS_CMD_SKIP_MONITOR    = 2048 /* "M" flag */
	REDIS_CMD_ASKING          = 4096 /* "k" flag */
	REDIS_CMD_FAST            = 8192 /* "F" flag */
	/* Command call flags, see call() function */
	REDIS_CALL_NONE      = 0
	REDIS_CALL_SLOWLOG   = 1
	REDIS_CALL_STATS     = 2
	REDIS_CALL_PROPAGATE = 4
	REDIS_CALL_FULL      = (REDIS_CALL_SLOWLOG | REDIS_CALL_STATS | REDIS_CALL_PROPAGATE)

	/* Units */
	UNIT_SECONDS      = 0
	UNIT_MILLISECONDS = 1

	REDIS_SET_NO_FLAGS = 0
	REDIS_SET_NX       = (1 << 0) /* Set if key not exists. */
	REDIS_SET_XX       = (1 << 1) /* Set if key exists. */

	REDIS_DEFAULT_DBNUM = 16

	/* Object types */
	REDIS_STRING = 0
	REDIS_LIST   = 1
	REDIS_SET    = 2
	REDIS_ZSET   = 3
	REDIS_HASH   = 4

	/* Objects encoding. Some kind of objects like Strings and Hashes can be
	 * internally represented in multiple ways. The 'encoding' field of the object
	 * is set to one of this fields for this object. */
	REDIS_ENCODING_RAW        = 0 /* Raw representation */
	REDIS_ENCODING_INT        = 1 /* Encoded as integer */
	REDIS_ENCODING_HT         = 2 /* Encoded as hash table */
	REDIS_ENCODING_ZIPMAP     = 3 /* Encoded as zipmap */
	REDIS_ENCODING_LINKEDLIST = 4 /* Encoded as regular linked list */
	REDIS_ENCODING_ZIPLIST    = 5 /* Encoded as ziplist */
	REDIS_ENCODING_INTSET     = 6 /* Encoded as intset */
	REDIS_ENCODING_SKIPLIST   = 7 /* Encoded as skiplist */
	REDIS_ENCODING_EMBSTR     = 8 /* Embedded sds string encoding */

	/* List related stuff */
	REDIS_HEAD = 0
	REDIS_TAIL = 1

	REDIS_SHARED_INTEGERS    = 10000
	REDIS_SHARED_BULKHDR_LEN = 32

	REDIS_HASH_KEY   = 1
	REDIS_HASH_VALUE = 2

	ZSKIPLIST_MAXLEVEL = 32
	ZSKIPLIST_P        = 0.25

	// Slowlog相关常量
	SLOWLOG_ENTRY_MAX_ARGC                 = 32    // 最大参数数量
	SLOWLOG_ENTRY_MAX_STRING               = 128   // 最大参数字符串长度
	CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN = 10000 // 默认阈值10毫秒
	CONFIG_DEFAULT_SLOWLOG_MAX_LEN         = 128   // 默认最多128条
)

type redisServer struct {
	//record the ip and port number of the redis server.
	ip   string
	port int
	//semaphore used to notify shutdown.
	shutDownCh    chan struct{}
	commandCh     chan redisClient
	closeClientCh chan redisClient
	done          atomic.Int32
	//record all connected clients.
	clients sync.Map
	//listen and process new connections.
	listen   net.Listener
	commands map[string]redisCommand
	db       []redisDb
	dbnum    int

	// Slowlog相关字段
	slowlog              *list // 复用现有adlist，参考: list *slowlog
	slowlogEntryId       int64 // 全局递增ID，参考: slowlog_entry_id
	slowlogLogSlowerThan int64 // 阈值（微秒），参考: slowlog_log_slower_than
	slowlogMaxLen        int64 // 最大记录数，参考: slowlog_max_len
}

type robj = redisObject

type redisObject struct {
	robjType int
	encoding int
	ptr      *interface{}
}

/*
*
跳表节点的定义
*/
type zskiplistNode struct {
	//记录元素的redis指针
	obj *robj
	//记录当前元素的数值，代表当前元素的优先级
	score float64
	//指向当前元素的前驱节点，即小于当前节点的元素
	backward *zskiplistNode
	//用一个zskiplistLevel数组维护本届点各层索引信息
	level []zskiplistLevel
}

type zskiplistLevel struct {
	//记录本层索引的前驱节点的指针
	forward *zskiplistNode
	//标识节点的本层索引需要跨几步才能到达该节点
	span int64
}

type zskiplist struct {
	//指向跳表的头节点
	header *zskiplistNode
	//指向跳表的尾节点
	tail *zskiplistNode
	//维护跳表的长度
	length int64
	//维护跳表当前索引的最高高度
	level int
}

/*
有序集合结构体
*/
type zset struct {
	dict map[string]*float64
	zsl  *zskiplist
}

func initServer() {
	log.Println("init redis server")
	server.ip = "localhost"
	server.port = 6379
	server.shutDownCh = make(chan struct{})
	server.closeClientCh = make(chan redisClient)
	server.commandCh = make(chan redisClient)

	createSharedObjects()
	server.db = make([]redisDb, server.dbnum)

	for j := 0; j < server.dbnum; j++ {
		server.db[j].id = j
		//server.db[j].dict = make(map[string]*robj)
		//server.db[j].expires = make(map[string]int64)
		server.db[j].dict = *dictCreate(&dbDictType, nil)
		server.db[j].expires = *dictCreate(&dbDictType, nil)
	}

	// 初始化slowlog
	slowlogInit()
}

// loadServerConfig - 加载服务器配置
// 优先从 redis.conf 文件读取配置，若文件不存在或配置项为空则使用默认值
func loadServerConfig() {
	log.Println("load redis server config")
	server.dbnum = REDIS_DEFAULT_DBNUM

	// 先设置默认值
	server.slowlogLogSlowerThan = CONFIG_DEFAULT_SLOWLOG_LOG_SLOWER_THAN
	server.slowlogMaxLen = CONFIG_DEFAULT_SLOWLOG_MAX_LEN

	// 尝试从配置文件读取
	configFile := "redis.conf"
	if _, err := os.Stat(configFile); err == nil {
		loadConfigFromFile(configFile)
	} else {
		log.Printf("Config file %s not found, using default values", configFile)
	}
}

// loadConfigFromFile - 从配置文件加载配置
func loadConfigFromFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Error opening config file: %v", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释行
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析配置项
		if err := parseConfigLine(line, lineNum); err != nil {
			log.Printf("Config parse error at line %d: %v", lineNum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading config file: %v", err)
	}

	log.Printf("Loaded config from %s: slowlog-log-slower-than=%d, slowlog-max-len=%d",
		filename, server.slowlogLogSlowerThan, server.slowlogMaxLen)
}

// parseConfigLine - 解析单行配置
func parseConfigLine(line string, lineNum int) error {
	// 分割配置项和值
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return fmt.Errorf("invalid config format: %s", line)
	}

	key := strings.ToLower(parts[0])
	value := parts[1]

	// 处理带引号的值
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		value = value[1 : len(value)-1]
	}

	switch key {
	case "slowlog-log-slower-than":
		if value == "" {
			return fmt.Errorf("slowlog-log-slower-than value cannot be empty")
		}
		val, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid slowlog-log-slower-than value: %s", value)
		}
		server.slowlogLogSlowerThan = val
		log.Printf("Config loaded: slowlog-log-slower-than = %d", val)

	case "slowlog-max-len":
		if value == "" {
			return fmt.Errorf("slowlog-max-len value cannot be empty")
		}
		val, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid slowlog-max-len value: %s", value)
		}
		if val <= 0 {
			return fmt.Errorf("slowlog-max-len must be positive: %d", val)
		}
		server.slowlogMaxLen = val
		log.Printf("Config loaded: slowlog-max-len = %d", val)

	case "port":
		if value == "" {
			return fmt.Errorf("port value cannot be empty")
		}
		val, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port value: %s", value)
		}
		server.port = val
		log.Printf("Config loaded: port = %d", val)

	case "databases":
		if value == "" {
			return fmt.Errorf("databases value cannot be empty")
		}
		val, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid databases value: %s", value)
		}
		server.dbnum = val
		log.Printf("Config loaded: databases = %d", val)
	}

	return nil
}

func acceptTcpHandler(conn net.Conn) {
	//the current server is being or has been shut down, and no new connections are being processed.
	if server.done.Load() == 1 {
		log.Println("the current service is being shut down. The connection is denied.")
		_ = conn.Close()

	}
	//init the redis client and handles network read and write events.
	c := createClient(conn)
	server.clients.Store(c.string(), c)
	go readQueryFromClient(c, server.closeClientCh, server.commandCh)

}

func createClient(conn net.Conn) *redisClient {
	c := redisClient{conn: conn, argc: 0, argv: make([]*robj, 0), multibulklen: -1}
	selectDb(&c, 0)
	return &c
}

func closeRedisServer() {
	log.Println("close listen and all redis client")
	_ = server.listen.Close()
	server.clients.Range(func(key, value any) bool {
		client := value.(*redisClient)
		_ = client.conn.Close()
		server.clients.Delete(key)
		return true
	})

	wg.Done()
}

func initServerConfig() {
	server.commands = make(map[string]redisCommand)

	populateCommandTable()
}

func populateCommandTable() {

	for i := 0; i < len(redisCommandTable); i++ {
		redisCommand := redisCommandTable[i]
		for _, f := range redisCommand.sflag {
			if f == 'w' {
				redisCommand.flag |= REDIS_CMD_WRITE
			} else if f == 'r' {
				redisCommand.flag |= REDIS_CMD_READONLY
			} else if f == 'm' {
				redisCommand.flag |= REDIS_CMD_DENYOOM
			} else if f == 'a' {
				redisCommand.flag |= REDIS_CMD_ADMIN
			} else if f == 'p' {
				redisCommand.flag |= REDIS_CMD_PUBSUB
			} else if f == 's' {
				redisCommand.flag |= REDIS_CMD_NOSCRIPT
			} else if f == 'R' {
				redisCommand.flag |= REDIS_CMD_RANDOM
			} else if f == 'S' {
				redisCommand.flag |= REDIS_CMD_SORT_FOR_SCRIPT
			} else if f == 'l' {
				redisCommand.flag |= REDIS_CMD_LOADING
			} else if f == 't' {
				redisCommand.flag |= REDIS_CMD_STALE
			} else if f == 'M' {
				redisCommand.flag |= REDIS_CMD_SKIP_MONITOR
			} else if f == 'K' {
				redisCommand.flag |= REDIS_CMD_ASKING
			} else if f == 'F' {
				redisCommand.flag |= REDIS_CMD_FAST
			} else {
				log.Panicln("Unsupported command flag")
			}

			server.commands[redisCommand.name] = redisCommand
		}

	}
}

func processCommand(c *redisClient) {
	//check the command table to see if the specified command exists.
	ptr := c.argv[0].ptr
	cmd, exists := server.commands[strings.ToUpper((*ptr).(string))]

	//assign the function of the command to "cmd".
	c.cmd = cmd
	c.lastCmd = cmd

	if !exists {
		c.conn.Write([]byte("-ERR unknown command\r\n"))
		return
	} else if (c.cmd.arity > 0 && c.cmd.arity != int64(c.argc)) ||
		int64(c.argc) < -(c.cmd.arity) {
		reply := "wrong number of arguments for " + (*ptr).(string) + " command"
		addReplyError(c, &reply)
		return
	}

	//invoke "call" to pass the parameters to the function pointed to by "cmd" for processing.
	call(c, REDIS_CALL_FULL)
}

func call(c *redisClient, flags int) {
	// 记录开始时间（微秒）
	start := time.Now().UnixMicro()

	// 执行命令
	c.cmd.proc(c)

	// 计算执行时长
	duration := time.Now().UnixMicro() - start

	// 记录慢查询（SLOWLOG 命令本身不记录）
	if flags&REDIS_CALL_SLOWLOG != 0 && c.cmd.name != "SLOWLOG" {
		slowlogPushEntryIfNeeded(c.argv, int(c.argc), duration)
	}

	//todo aof use flags
}

func (o *robj) String() string {
	return fmt.Sprintf("%v", *o.ptr)
}
