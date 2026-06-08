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

// stringmatchlen 实现 Redis 的 glob 风格模式匹配
// 支持: * (任意序列), ? (单字符), [abc] [^abc] [a-z] (字符类), \ (转义)
// nocase=true 时大小写不敏感
// 复刻自 Redis util.c stringmatchlen
func stringmatchlen(pattern string, patternLen int, s string, sLen int, nocase bool) bool {
	pi, si := 0, 0
	for patternLen > 0 && sLen > 0 {
		switch pattern[pi] {
		case '*':
			// 合并连续 *,等价于一个 *
			for patternLen > 1 && pattern[pi+1] == '*' {
				pi++
				patternLen--
			}
			// 末尾的 * 匹配剩余任意字符
			if patternLen == 1 {
				return true
			}
			// 递归尝试 * 匹配 0..N 个字符
			for sLen > 0 {
				if stringmatchlen(pattern[pi+1:], patternLen-1, s[si:], sLen, nocase) {
					return true
				}
				si++
				sLen--
			}
			return false
		case '?':
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
				// 字符类未闭合 (如 "[abc"),回退让外层按字面量处理
				if patternLen == 0 {
					pi--
					patternLen++
					break
				}
				if patternLen >= 2 && pattern[pi] == '\\' {
					pi++
					patternLen--
					if pattern[pi] == s[si] {
						match = true
					}
				} else if pattern[pi] == ']' {
					break
				} else if patternLen >= 3 && pattern[pi+1] == '-' { //针对[a-c]的情况
					//获取起始位
					start := pattern[pi]
					//获取结束位
					end := pattern[pi+2]
					//获取待匹配字符
					c := s[si]
					//判断起始位是否大于结束位,例如[c-a]需要进行交换
					if start > end {
						t := start
						start = end
						end = t
					}
					//忽略大小写
					if nocase {
						start = toLowerByte(start)
						end = toLowerByte(end)
						c = toLowerByte(c)
					}
					//匹配字符串移到末尾,让外部递增结束循环
					pi += 2
					//匹配长度移动到最后一位,让外部结束虚幻
					patternLen -= 2

					if c >= start && c <= end {
						match = true
					}

				} else {
					if nocase {
						if toLowerByte(pattern[pi]) == toLowerByte(s[si]) {
							match = true
						}
					} else {
						if pattern[pi] == s[si] {
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
			if nocase {
				if toLowerByte(pattern[pi]) != toLowerByte(s[si]) {
					return false
				}
			} else {
				if pattern[pi] != s[si] {
					return false
				}
			}
			si++
			sLen--
		}
		pi++
		patternLen--
		// 字符串耗尽,跳过剩余的 * (它们都能匹配空)
		if sLen == 0 {
			for patternLen > 0 && pattern[pi] == '*' {
				pi++
				patternLen--
			}
			break
		}
	}
	return patternLen == 0 && sLen == 0
}

// stringmatch 是 stringmatchlen 的便捷封装,用于 Go 字符串直接匹配
func stringmatch(pattern, s string, nocase bool) bool {
	return stringmatchlen(pattern, len(pattern), s, len(s), nocase)
}

// toLowerByte 仅处理 ASCII 大写转小写,与 Redis tolower() 行为一致
func toLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}
