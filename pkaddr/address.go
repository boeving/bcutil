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

// ErrChecksum indicates that the checksum of a check-encoded string
// does not verify against the checksum.
var ErrChecksum = errors.New("checksum error")

// ErrInvalidFormat 无效的地址格式。
var ErrInvalidFormat = errors.New("invalid format: flag prefix missing")

// 基本常量。
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
type Address struct {
	Hash PKHash
	// 前缀标识
	// 仅限：SignFlag|MultiSignFlag
	Flag string
}

//
// NewAddress 创建一个公钥地址实例。
// @sf 是否为普通地址前缀标识（非多签名地址）。
//
func NewAddress(h *PKHash, sf bool) *Address {
	f := SignFlag

	if !sf {
		f = MultiSignFlag
	}
	addr := Address{Hash: *h, Flag: f}

	return &addr
}

//
// IsMultiSign 是否为多签名地址。
//
func (a *Address) IsMultiSign() bool {
	return a.Flag == MultiSignFlag
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
//
func (a *Address) Decode(addr string) error {
	f, h := addrPair(addr)
	if f == "" {
		return ErrInvalidFormat
	}
	dec := base58.Decode(h)

	var cksum [LenChecksum]byte
	n := len(dec) - LenChecksum
	copy(cksum[:], dec[n:])

	if checksum(append([]byte(f), dec[:n])) != cksum {
		return ErrChecksum
	}
	a.Flag = f
	copy(a.Hash[:], dec[:n])

	return nil
}

//
// 切分地址字符串为两片：标识，正文。
// 正文部分由Base58编码而来。
//
func addrPair(addr string) (string, string) {
	if strings.HasPrefix(addr, SignFlag) {
		return SignFlag, addr[len(SignFlag):]
	}
	if strings.HasPrefix(addr, MultiSignFlag) {
		return MultiSignFlag, addr[len(MultiSignFlag):]
	}
	return "", addr
}

//
// String 输出字符串表示，即Encode的结果。
//
func (a *Address) String() string {
	return a.Encode()
}

//
// UnmarshalText 反序列化接口实现。
// 主要用于JSON格式数据解码（json.Unmarshal）。
//
func (a *Address) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	err := a.Decode(string(text))
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
