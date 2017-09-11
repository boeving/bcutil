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
// Decode 解码区块链地址。
// 还原用户地址为公钥Hash。
// 解码失败返回nil
//
func Decode(s58 string) Hash20 {

}

//
// Encode 编码区块链地址。
// 由公钥Hash到用户地址的编码。
//
func Encode(b20 []byte) string {

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
