package cbaddr

import (
	"crypto/sha256"
	"hash"

	"golang.org/x/crypto/ripemd160"
)

// 基本常量。
const (
	Prefix  = "CX" // 普通地址前缀
	Prefix3 = "cx" // 多重签名地址前缀
)

// Hash20 160位/20字节Hash序列。
// 一般指Base58编码前且未附带前缀的区块链地址。
type Hash20 []byte

//
// UnmarshalText 序列解码接口实现（如JSON:Unmarshal），
// 采用Decode的解码结果。
//
func (h *Hash20) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*h = nil
		return nil
	}
	v, err := Decode(string(text))
	if err != nil {
		return err
	}
	*h = v
	return nil
}

//
// Decode 解码区块链地址。
// 还原用户地址为公钥Hash，用户地址为Base58编码格式。
//
func Decode(s string) (Hash20, error) {

}

//
// Encode 编码区块链地址。
// 将公钥Hash编码为用户地址（Base58）。
//
func Encode(b Hash20) string {

}

// Calculate the hash of hasher over buf.
func calcHash(buf []byte, hasher hash.Hash) []byte {
	hasher.Write(buf)
	return hasher.Sum(nil)
}

// Hash160 calculates the hash ripemd160(sha256(b)).
func Hash160(buf []byte) []byte {
	return calcHash(calcHash(buf, sha256.New()), ripemd160.New())
}
