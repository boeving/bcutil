package sha1x

///////////////
/// 基本工具集
///////////////////////////////////////////////////////////////////////////////

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
)

//
// Sum256 复合校验和。
// 算法：sha1(sha256(..))
//
// 单纯的sha1已不可靠，但仍可用于生成尽量短的标识串。
// 因此采用嵌套sha256的方式计算。
// 参见：
// https://security.googleblog.com/2017/02/announcing-first-sha1-collision.html
//
func Sum256(data []byte) [20]byte {
	h := sha256.Sum256(data)
	return sha1.Sum(h[:])
}

//
// Sum512 复合交易和。
// 综合sha512的安全性和sha1的短长度优势。
// flag序列与sha512的结果合并。
//
func Sum512(flag, data []byte) [20]byte {
	h := sha512.Sum512(data)
	buf := h[:]
	if flag != nil {
		buf = append(flag, buf...)
	}
	return sha1.Sum(buf)
}
