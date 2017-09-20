package pkaddr

import (
	"crypto/sha256"
	"hash"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ripemd160"
)

//
// Auth 签名授权。
// 用法：
// 外部传递公钥、对公钥哈希的签名以及地址前缀。
// 验证：
//  1. 从公钥和地址前缀构造公钥地址；
//  2. 检查公钥地址与目标地址是否匹配；
//  3. 验证对公钥哈希的签名数据。
// 注：
// 验证数据不包含地址前缀标识。
//
type Auth struct {
	Address
	PubKey ed25519.PublicKey
}

//
// NewAuth 创建一个签名授权实例。
//
func NewAuth(pk ed25519.PublicKey, flag string) *Auth {
	hash := Hash160(pk)

	return &Auth{
		Hash:   hash,
		Flag:   flag,
		PubKey: pk,
	}
}

//
// Address 获取公钥地址。
// 即Base58编码的区块链地址。
//
func (au *Auth) Address() string {
	return au.Encode()
}

//
// Verify 签名验证。
// 实际上为对内部公钥哈希数据进行签名验证。
//
func (au *Auth) Verify(sig []byte) bool {
	return ed25519.Verify(au.PubKey, au.Hash[:], sig)
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

//
// Calculate the hash of hasher over buf.
//
func calcHash(buf []byte, hasher hash.Hash) []byte {
	hasher.Write(buf)
	return hasher.Sum(nil)
}

//
// Hash160 calculates the hash ripemd160(sha256(b)).
// 主要用于对公钥的Hash计算。
//
func Hash160(buf []byte) PKHash {
	var hash PKHash

	hash.SetBytes(
		calcHash(calcHash(buf, sha256.New()), ripemd160.New()))

	return hash
}
