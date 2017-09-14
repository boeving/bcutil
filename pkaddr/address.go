//
// Package pkaddr 公钥地址相关的特性操作。
//
package pkaddr

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"

	"github.com/qchen-zh/pputil/base58"
	"golang.org/x/crypto/ripemd160"
)

// ErrChecksum 地址校验和验证不合格。
var ErrChecksum = errors.New("checksum error")

// 基本常量定义。
const (
	SignFlag      = "CX" // 普通地址前缀标识
	MultiSignFlag = "cx" // 多重签名地址前缀标识
	LenChecksum   = 4    // 校验码长度
)

// PKHash 160位/20字节Hash序列。
// 公钥地址Base58编码前未附带前缀和校验码的公钥哈希。
type PKHash [20]byte

//
// SetBytes 转换字节序列为数组值。
// 切片长度必须和数组长度相等，否则返回nil。
//
func (p *PKHash) SetBytes(bs []byte) *PKHash {
	if len(bs) != len(*p) {
		return nil
	}
	copy((*p)[:], bs)
	return p
}

//
// SetString 从字符串表示设置序列值。
// 按字符串从左到右的顺序对应赋值（big-endian）。
// 16进制字符串不含前导0x或0X标识。
//
func (p *PKHash) SetString(s string, base int) *PKHash {
	i := new(big.Int)
	if _, ok := i.SetString(s, base); !ok {
		return nil
	}
	copy((*p)[:], i.Bytes())
	return p
}

//
// String 显示为十六进制串，附 0x 前缀。
//
func (p *PKHash) String() string {
	fmt.Printf("%#x", *p)
}

// Address 公钥地址。
// 原则上Flag可任意指定，但本类型的UnmarshalText方法除外。
type Address struct {
	Hash PKHash
	// 前缀标识
	// 如：SignFlag，MultiSignFlag
	Flag string
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

	if checksum(append([]byte(f), dec[:n])) != cksum {
		return ErrChecksum
	}
	a.Flag = flag
	copy(a.Hash[:], dec[:n])

	return nil
}

//
// Flag 提取地址前缀标识。
// 仅限于已定义的 SignFlag 和 MultiSignFlag。
//
// （注：Base58编码的长度并不与数据源的长度一一对应，因此无法提取未知前缀标识）。
//
func Flag(addr string) string {
	if strings.HasPrefix(addr, SignFlag) {
		return SignFlag
	}
	if strings.HasPrefix(addr, MultiSignFlag) {
		return MultiSignFlag
	}
	return ""
}

//
// String 输出字符串表示，即Encode的结果。
//
func (a *Address) String() string {
	return a.Encode()
}

//
// UnmarshalText 编码解组接口实现。
//
// 用于JSON格式数据解码（json.Unmarshal）。
// 仅限于已定义的前缀标识（Flag()的返回值）的地址。
//
func (a *Address) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	s := string(text)

	err := a.Decode(s, a.Flag(s))
	if err != nil {
		return err
	}
	return nil
}

// 计算校验和。
// checksum: first some bytes of sha256^2
func checksum(input []byte) (cksum [LenChecksum]byte) {
	h := sha256.Sum256(input)
	h2 := sha256.Sum256(h[:])
	copy(cksum[:], h2[:LenChecksum])
	return
}

// Calculate the hash of hasher over buf.
func calcHash(buf []byte, hasher hash.Hash) []byte {
	hasher.Write(buf)
	return hasher.Sum(nil)
}

// Hash160 calculates the hash ripemd160(sha256(b)).
// 主要用于对公钥的Hash计算。
func Hash160(buf []byte) []byte {
	return calcHash(calcHash(buf, sha256.New()), ripemd160.New())
}
