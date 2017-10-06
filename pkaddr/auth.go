package pkaddr

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"time"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ripemd160"
)

//
// Auth 签名授权。
// 用法：
// 外部传递必要的验证数据：
//  - 公钥
//  - 最新时间戳（JSON）
//  - 对 sha256(时间戳+公钥) 的签名，时间戳取Uinx毫秒数大端字节序
//  - 地址前缀
// 验证：
//  1. 检查时间戳是否符合要求；
//  2. 从公钥和地址前缀构造公钥地址，检查是否与目标地址匹配；
//  3. 验证 sha256(时间戳+公钥) 的签名数据。
//     时间戳取Unix时间毫秒数（1970.1.1～至今）
// 注：
// 验证数据不包含地址前缀标识。
//
type Auth struct {
	addr      Address
	timestamp time.Time
	pubkey    ed25519.PublicKey
}

//
// Address 设置地址。
//
func (au *Auth) Address(pk ed25519.PublicKey, flag string) {
	au.addr = Address{
		Hash: Hash160(pk),
		Flag: flag,
	}
	au.pubkey = pk
}

//
// Timestamp 设置时间戳。
// @js 时间戳的JSON表示（含外围双引号）。
//
func (au *Auth) Timestamp(js string) error {
	return au.timestamp.UnmarshalJSON([]byte(js))
}

//
// CheckTime 检查时间符合误差。
// @d 与当前时间的容许误差。
//
func (au *Auth) CheckTime(d time.Duration) bool {
	d0 := time.Since(au.timestamp)
	if d0 < 0 {
		d0 = -d0
	}
	return d0 <= d
}

//
// CheckAddr 检查地址是否符合。
//
func (au *Auth) CheckAddr(addr Address) bool {
	return au.addr == addr
}

//
// CheckSig 验证签名。
// 目标数据为对 sha256(时间戳Uinx毫秒数+公钥) 的签名。
// 毫秒数值采用8字节大端字节序。
//
func (au *Auth) CheckSig(sig []byte) bool {
	ms := au.timestamp.UnixNano() / 1000000
	b := make([]byte, 8)

	binary.BigEndian.PutUint64(b, uint64(ms))
	b = append(b, au.pubkey...)

	msg := sha256.Sum256(b)
	return ed25519.Verify(au.pubkey, msg[:], sig)
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
