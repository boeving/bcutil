package pkaddr

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"hash"
	"math/big"

	"golang.org/x/crypto/ripemd160"
)

// Auth 椭圆曲线授权。
type Auth struct {
	Address
	pubkey *ecdsa.PublicKey
}

//
// NewAuth 创建一个授权验证对象。
//
func NewAuth(addr Address) *Auth {
	return &Auth{addr, nil}
}

//
// PubKey 设置公钥。
// 会验证公钥的Hash是否与地址匹配。
// 这是授权检查首先需要进行的第一步操作。
//
func (ea *Auth) PubKey(pub *ecdsa.PublicKey) bool {

}

//
// Verify 授权验证。
// 实际上为对内部公钥哈希数据进行签名验证。
//
// @r, @s 对公钥哈希签名获得的两个值。
//
func (ea *Auth) Verify(r, s *big.Int) bool {

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
func Hash160(buf []byte) PKHash {
	var hash PKHash

	hash.SetBytes(
		calcHash(calcHash(buf, sha256.New()), ripemd160.New()))

	return hash
}
