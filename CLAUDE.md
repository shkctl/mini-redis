# mini-redis 项目约束

## 架构风格
- 面向过程编程，复刻 Redis C 源码结构（非 OOP）
- 全局变量 `server` (redisServer) 作为核心状态容器
- 新功能字段统一挂载到 redisServer 结构体

## 文件职责
- `redis.go`：核心结构体（redisServer/robj）、常量、初始化函数
- `command.go`：命令注册表 redisCommandTable、共享对象
- `adlist.go`：双向链表（listCreate/listAddNodeHead/listDelNode 等）
- `dict.go`：字典（dictCreate/dictAdd/dictFind 等）
- `t_hash.go` / `t_list.go` / `t_zset.go`：各数据类型实现
- `slowlog.go`：慢查询功能实现
- `server.go`：服务器启动和事件循环
- `client.go`：客户端连接处理
- `networking.go`：网络 I/O 和协议响应
- `db.go`：数据库操作
- `object.go`：对象创建和编码
- `util.go`：工具函数

## 新功能开发模式
按以下顺序开发：
1. 数据结构定义（新建 xxx.go 或加到 redis.go）
2. 常量定义（redis.go const 块）
3. redisServer 添加字段
4. loadServerConfig() 设置默认值
5. initServer() 调用初始化函数
6. 核心逻辑函数（xxx.go）
7. 命令注册（command.go 的 redisCommandTable）
8. 命令处理函数（xxx.go）
9. 测试文件 xxx_test.go

## 类型系统
- 所有值统一用 robj (redisObject) 表示
- robj.ptr 是 *interface{}，需要类型断言
- 字符串提取：`(*c.argv[i].ptr).(string)`
- 整数提取：先取字符串再 `strconv.ParseInt(s, 10, 64)`
- 创建字符串对象：`createStringObject(&s, len(s))`
- 创建整数对象：`createStringObjectFromLongLong(v)`

## 命令处理函数模式
- 参数校验：检查 c.argc，不匹配则 `addReplyError(c, &errMsg)`
- 子命令分发：`strings.ToUpper(subCmd)` + switch
- 响应协议：`addReplyMultiBulkLen` / `addReplyBulk` / `addReplyLongLong` / `addReply(c, shared.ok)`
- 错误消息与 Redis 原文一致

## 测试约定
- 每个测试调用 setup 函数初始化环境
- 初始化顺序：createSharedObjects() → loadServerConfig() → initServerConfig() → initServer()
- 测试分类：_Basic / _EdgeCase / _Disabled / TestRedisCompat_ / TestIntegration_
