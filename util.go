package main

import (
	"math"
	"strconv"
)

func string2l(s *string, len int, lval *int64) bool {
	llval, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return false
	}
	if llval < math.MinInt64 || llval > math.MaxInt64 {
		return false
	}

	*lval = llval
	return true
}

// stringmatchlen 复刻 Redis util.c 的 glob 风格匹配,支持:
//   - `*` 匹配任意长度子串(含 0)
//   - `?` 匹配任意单字符
//   - `[abc]` / `[a-z]` / `[^abc]` 字符类
//   - `\x` 转义
//
// nocase 非 0 时进行大小写不敏感比较。
func stringmatchlen(pattern string, patternLen int, s string, sLen int, nocase int) bool {
	pi, si := 0, 0
	for patternLen > 0 {
		switch pattern[pi] {
		case '*':
			// 合并连续 '*'
			for patternLen > 1 && pattern[pi+1] == '*' {
				pi++
				patternLen--
			}
			// 模式以 '*' 收尾:无条件匹配
			if patternLen == 1 {
				return true
			}
			// 贪婪尝试:对剩余字符串的每个起点递归匹配 pattern[pi+1:]
			for sLen > 0 {
				if stringmatchlen(pattern[pi+1:], patternLen-1, s[si:], sLen, nocase) {
					return true
				}
				si++
				sLen--
			}
			return false
		case '?':
			if sLen == 0 {
				return false
			}
			si++
			sLen--
		case '[':
			pi++
			patternLen--
			negate := patternLen > 0 && pattern[pi] == '^'
			if negate {
				pi++
				patternLen--
			}
			match := false
			for {
				if patternLen == 0 {
					// 异常:无闭合 ']',回退一位与 Redis 行为对齐
					pi--
					patternLen++
					break
				}
				if pattern[pi] == '\\' && patternLen >= 2 {
					pi++
					patternLen--
					if pattern[pi] == s[si] {
						match = true
					}
				} else if pattern[pi] == ']' {
					break
				} else if patternLen >= 3 && pattern[pi+1] == '-' {
					start := pattern[pi]
					end := pattern[pi+2]
					c := s[si]
					if start > end {
						start, end = end, start
					}
					if nocase != 0 {
						start = toLowerByte(start)
						end = toLowerByte(end)
						c = toLowerByte(c)
					}
					pi += 2
					patternLen -= 2
					if c >= start && c <= end {
						match = true
					}
				} else {
					if nocase == 0 {
						if pattern[pi] == s[si] {
							match = true
						}
					} else {
						if toLowerByte(pattern[pi]) == toLowerByte(s[si]) {
							match = true
						}
					}
				}
				pi++
				patternLen--
			}
			if negate {
				match = !match
			}
			if !match {
				return false
			}
			si++
			sLen--
		case '\\':
			if patternLen >= 2 {
				pi++
				patternLen--
			}
			fallthrough
		default:
			if nocase == 0 {
				if pattern[pi] != s[si] {
					return false
				}
			} else {
				if toLowerByte(pattern[pi]) != toLowerByte(s[si]) {
					return false
				}
			}
			si++
			sLen--
		}
		pi++
		patternLen--
		if sLen == 0 {
			// 字符串用尽时跳过剩余的 '*',兼容 "abc*"
			for patternLen > 0 && pattern[pi] == '*' {
				pi++
				patternLen--
			}
			break
		}
	}
	return patternLen == 0 && sLen == 0
}

// stringmatch 是 stringmatchlen 的便捷封装,用模式整串与目标整串比较。
func stringmatch(pattern string, s string, nocase int) bool {
	return stringmatchlen(pattern, len(pattern), s, len(s), nocase)
}

func toLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
