package main

import "strconv"

func addReply(c *redisClient, reply *string) {
	c.conn.Write([]byte(*reply))
}

func addReplyBulk(c *redisClient, obj *robj) {
	if obj.encoding == REDIS_ENCODING_EMBSTR {
		value := (*obj.ptr).(string)
		c.conn.Write([]byte("$" + strconv.Itoa(len(value)) + *shared.crlf + value + *shared.crlf))
	} else if obj.encoding == REDIS_ENCODING_INT {
		num := (*obj.ptr).(int64)
		numStr := strconv.FormatInt(num, 10)
		c.conn.Write([]byte("$" + strconv.Itoa(len(numStr)) + *shared.crlf + numStr + *shared.crlf))
	}

}

func addReplyError(c *redisClient, s *string) {
	c.conn.Write([]byte("-ERR " + *s + "\r\n"))
}

func addReplyLongLong(c *redisClient, ll int64) {
	if ll == 0 {
		addReply(c, shared.czero)
	} else if ll == 1 {
		addReply(c, shared.cone)
	} else {
		addReplyLongLongWithPrefix(c, ll, ":")
	}
}

func addReplyLongLongWithPrefix(c *redisClient, ll int64, prefix string) {
	c.conn.Write([]byte(prefix + strconv.FormatInt(ll, 10) + "\r\n"))
}

func addReplyMultiBulkLen(c *redisClient, length int64) {
	if length < REDIS_SHARED_BULKHDR_LEN {
		s := (*shared.bulkhdr[length].ptr).(string)
		addReply(c, &s)
	} else {
		addReplyLongLongWithPrefix(c, length, "*")
	}
}

// addReplyBulkLongLong 将整数以 RESP bulk string 形式回复
// 用于 SCAN 等命令返回 cursor:协议要求 `$<len>\r\n<num>\r\n`
func addReplyBulkLongLong(c *redisClient, ll int64) {
	s := strconv.FormatInt(ll, 10)
	c.conn.Write([]byte("$" + strconv.Itoa(len(s)) + *shared.crlf + s + *shared.crlf))
}
