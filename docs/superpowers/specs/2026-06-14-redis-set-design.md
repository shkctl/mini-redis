# Redis SET 集合命令实现设计

日期: 2026-06-14
状态: 已批准 (方案 A)
对应 Redis 源码: `src/intset.h` / `src/intset.c` / `src/t_set.c`

## 1. 目标

在 mini-redis 中复刻 Redis 的 SET 类型,实现完整双编码(intset + hashtable),并通过 `setType*` 抽象层屏蔽编码差异。本轮实现基础六件套命令: `SADD` / `SREM` / `SMEMBERS` / `SISMEMBER` / `SCARD` / `SPOP`。

## 2. 双编码设计

Redis Set 全是整数且元素数 ≤ 512 时用 **intset**(紧凑有序数组),否则用 **hashtable**(dict,value 为 NULL)。

### 2.1 编码升级规则(intset → HT,单向)

插入元素时:
- 当前 intset 且 新元素为整数 且 元素数 < `REDIS_SET_MAX_INTSET_ENTRIES`(512) → 继续 intset
- 否则 → `setTypeConvert` 升级成 HT,再插入
- 当前 HT → 直接 map 写入

### 2.2 intset 的 Go 表示(方案 A)

保留 Redis 的 `encoding` 字段(INT16/32/64),数据用 `[]int64` 存储:

```go
type intset struct {
    encoding int    // INTSET_ENC_INT16 / INT32 / INT64
    length   int
    contents []int64 // 逻辑上按 encoding 位宽读取,保持有序
}
```

**取舍**: 守住 intset 的算法精华(有序 + 二分查找 + 位宽升级触发),放弃 C 柔性数组的字节级紧凑(GC 语言本也难真紧凑)。这是有意识的学习简化。

## 3. 文件布局

| 新文件 | 职责 | 对应 Redis |
|--------|------|-----------|
| `intset.go` | intset 数据结构与算法 | `intset.c` |
| `t_set.go` | setType 抽象层 + 6 个命令处理函数 + 编码转换 | `t_set.c` |

测试文件: `intset_test.go`、`t_set_test.go`,遵循项目 `_Basic`/`_EdgeCase`/`TestRedisCompat_` 分类命名。

## 4. setType* 抽象层 API

命令处理函数**只调 setType\*,不直接碰 intset 或 map**:

| 函数 | 说明 |
|------|------|
| `createSetObject()` | 创建 HT 编码集合(`map[string]struct{}`) |
| `createIntsetObject()` | 创建 intset 编码集合 |
| `setTypeCreate(value *robj) *robj` | 按首元素类型选 intset/HT(整数→intset,否则→HT) |
| `setTypeAdd(o, value) int` | 添加,内部处理编码升级。返回 1=新增 0=已存在 |
| `setTypeRemove(o, value) int` | 删除。返回 1=删除 0=不存在 |
| `setTypeIsMember(o, value) bool` | 判断成员 |
| `setTypeSize(o) int64` | 元素数 |
| `setTypeRandomElement(o) *robj` | 随机取一个(SPOP 用) |
| `setTypeInitIterator(o) *setTypeIterator` | 创建迭代器 |
| `setTypeNext(itor) (*robj, bool)` | 取下一个元素 |
| `setTypeLookupWriteOrCreate(c, key) *robj` | 查找或创建(类比 hashTypeLookupWriteOrCreate) |
| `setTypeConvert(o)` | intset → HT 单向升级 |

hashtable 编码用 `map[string]struct{}`(语义上无值,比 t_hash 的 `map[string]*robj` 更贴合集)。

## 5. 命令实现映射

| 命令 | arity | 核心调用 | 返回 |
|------|-------|---------|------|
| `SADD key m1 m2...` | -3 | setTypeCreate + 循环 setTypeAdd | 新增个数(int64) |
| `SREM key m1 m2...` | -3 | 循环 setTypeRemove,空则 dbDelete | 删除个数(int64) |
| `SMEMBERS key` | 2 | 迭代器遍历 + addReplyMultiBulkLen | 全部成员(multibulk) |
| `SISMEMBER key m` | 3 | setTypeIsMember | 0/1 |
| `SCARD key` | 2 | setTypeSize | 元素数(int64) |
| `SPOP key` | 2 | setTypeRandomElement + setTypeRemove | 被弹出的成员(bulk) |

SPOP 本轮仅实现弹出一个成员(无 count 参数)。

## 6. 常量

`redis.go` 已定义 `REDIS_SET = 2`、`REDIS_ENCODING_INTSET = 6`、`REDIS_ENCODING_HT = 2`。
新增:
- `REDIS_SET_MAX_INTSET_ENTRIES = 512`
- `INTSET_ENC_INT16 / INT32 / INT64` 位宽常量
- `intsetValueEncoding(int64) int` —— 按值大小返回所需位宽

## 7. 命令注册

`command.go` 的 `redisCommandTable` 追加:

```go
{name: "SADD", proc: saddCommand, arity: -3, sflag: "wmF", flag: 0},
{name: "SREM", proc: sremCommand, arity: -3, sflag: "wF", flag: 0},
{name: "SMEMBERS", proc: smembersCommand, arity: 2, sflag: "r", flag: 0},
{name: "SISMEMBER", proc: sismemberCommand, arity: 3, sflag: "rF", flag: 0},
{name: "SCARD", proc: scardCommand, arity: 2, sflag: "rF", flag: 0},
{name: "SPOP", proc: spopCommand, arity: 2, sflag: "wRs", flag: 0},
```

## 8. 错误消息(与 Redis 一致)

- `wrong number of arguments for 'sadd' command`
- `-WRONGTYPE Operation against a key holding the wrong kind of value`(复用 `shared.wrongtypeerr`)

## 9. 开发节奏(TDD + 人机协作)

1. intset.go: 数据结构 + `intsetValueEncoding` + `createIntsetObject`
2. **TODO(human)**: `intsetAdd` 核心算法(二分查找定位 + 已存在判断 + 升级触发)—— intset 灵魂,留给手写
3. intset.go 其余: `intsetRemove` / `intsetSearch` / `intsetRandom`
4. t_set.go: setType 抽象层
5. 命令处理函数逐个实现 + 注册
6. 每个命令按 `_Basic` → `_EdgeCase` → `TestRedisCompat_` 写测试

## 10. 验收标准

- `go build ./...` 通过
- `go test ./...` 全绿
- intset 编码下 SADD/SREM/SISMEMBER 行为正确
- 插入非整数元素自动升级到 HT,后续操作仍正确
- 元素超过 512 自动升级到 HT
- SREM 清空后 key 被删除(类比 hdel 行为)
- WRONGTYPE 错误消息与 Redis 一致
