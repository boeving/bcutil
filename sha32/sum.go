package sha32

///////////////
// 基本工具集
// 强化安全，使得256位仅能通过暴力碰撞实施攻击。
// 注：假设sha512未被破解。
//
// 考虑运算性能，未使用SHA-3系列。
///////////////////////////////////////////////////////////////////////////////

import (
	"crypto/sha256"
	"crypto/sha512"
)

//
// sha256 + sha512 复合校验和。
// 算法：sha256( sha512(..) )
// 安全参考：
// https://security.googleblog.com/2017/02/announcing-first-sha1-collision.html
//
func Sum(data []byte) [32]byte {
	h := sha512.Sum512(data)
	return sha256.Sum256(h[:])
}

//
// 前置标记的复合校验和。
// 算法：sha256( flag + sha512(...) )
//
func Sumf(flag, data []byte) [20]byte {
	h := sha512.Sum512(data)
	buf := h[:]
	if flag != nil {
		buf = append(flag, buf...)
	}
	return sha256.Sum256(buf)
}
