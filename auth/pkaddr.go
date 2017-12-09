package auth

/////////////////
// 公钥地址操作
// 包含对公钥哈希的签名验证（Auth）。
///////////////////////////////////////////////////////////////////////////////

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

	"github.com/qchen-zh/pputil/base58"
)

// ErrChecksum 地址校验和验证不合格。
var ErrChecksum = errors.New("checksum error")

// LenChecksum 校验码长度。
const LenChecksum = 4

// PKHash 160位/20字节公钥哈希。
// 公钥地址Base58编码前未附带前缀和校验码的公钥哈希。
type PKHash [20]byte

//
// SetBytes 从字节切片设置公钥哈希。
// 切片长度必须公钥哈希长度相等，否则返回nil。
//
func (p *PKHash) SetBytes(bs []byte) error {
	if len(bs) != len(*p) {
		return fmt.Errorf("bs's length must %d", len(*p))
	}
	copy((*p)[:], bs)
	return nil
}

//
// SetString 从字符串设置公钥哈希。
// 按字符串从左到右的顺序对应赋值（big-endian）。
// 16进制字符串可含前导0x或0X标识（或不含）。
//
func (p *PKHash) SetString(s string, base int) error {
	if base == 16 && len(s) > 2 && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	i := new(big.Int)
	if _, ok := i.SetString(s, base); !ok {
		return errors.New(s + " is invalid number characters")
	}
	copy((*p)[:], i.Bytes())
	return nil
}

//
// String 显示为十六进制串，附 0x 前缀。
//
func (p *PKHash) String() string {
	return fmt.Sprintf("%#x", *p)
}

// Address 公钥地址。
// 原则上Flag可任意指定，但本类型的UnmarshalText方法除外。
type Address struct {
	Hash PKHash // 公钥哈希
	Flag string // 前缀标识
}

//
// Encode 公钥哈希编码为公钥地址。
// 构成&流程：
//  1. 前缀标识 + 公钥哈希 => 校验码
//  2. 公钥哈希 + 校验码   => 地址值（Base58）
//  3. 前缀标识 + 地址值   => 公钥地址
//
func (a *Address) Encode() string {
	b := make([]byte, 0, len(a.Flag)+len(a.Hash)+LenChecksum)

	b = append(b, a.Flag...)
	b = append(b, a.Hash[:]...)

	cksum := checksum(b)
	b = append(b, cksum[:]...)

	return a.Flag + base58.Encode(b[len(a.Flag):])
}

//
// Decode 解码公钥地址。
// @flag 必须与编码时的值一致，
//
func (a *Address) Decode(s, flag string) error {
	dec := base58.Decode(s[len(flag):])
	n := len(dec) - LenChecksum

	var cksum [LenChecksum]byte
	copy(cksum[:], dec[n:])

	if checksum(append([]byte(flag), dec[:n]...)) != cksum {
		return ErrChecksum
	}
	a.Flag = flag
	copy(a.Hash[:], dec[:n])

	return nil
}

//
// String 输出字符串表示，即Encode的结果。
//
func (a *Address) String() string {
	return a.Encode()
}

//
// 计算校验和。
// checksum: first some bytes of sha256^2
//
func checksum(input []byte) (cksum [LenChecksum]byte) {
	h := sha256.Sum256(input)
	h2 := sha256.Sum256(h[:])
	copy(cksum[:], h2[:LenChecksum])
	return
}
