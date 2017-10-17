package bc25

import (
	"encoding/binary"
)

//
// 编码5字节单元。
// 返回一个8字节固定长度的字节序列。
// len(src) == 8，低5位有效
//
func encode(src []byte, chs string) []byte {
	var buf [8]byte

	val := binary.BigEndian.Uint64(src)

	for i := 7; i >= 0; i-- {
		buf[i] = chs[val&0x1f]
		val >>= 5
	}
	return buf[:]
}

//
// 解码8字符序列。
// 返回一个5字节固定长度的序列切片。
// len(data) == 8
//
func decode(data string, list [256]byte) []byte {
	var val uint64
	for i := 0; i < 8; i++ {
		val |= uint64(list[data[i]])
		val <<= 5
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], val>>5)

	return buf[3:]
}

func Encode(data []byte) string {
	cnt := len(data) / 5
	buf := make([]byte, 0, cnt*8)
	tmp := make([]byte, 8)

	for i := 0; i < cnt; i++ {
		off := i * 5
		copy(tmp[3:], data[off:off+5])
		buf = append(buf, encode(tmp, alphabet)...)
	}
	return string(buf)
}

func Decode(data string) []byte {
	cnt := len(data) / 8
	buf := make([]byte, 0, cnt*5)

	for i := 0; i < cnt; i++ {
		off := i * 8
		buf = append(buf, decode(data[off:off+8], b32)...)
	}
	return buf
}

// KRQWWZJANF4CAZDPO7XCA7DPEB4GQZJAMNXXA8JAMNSW67DFOIQGC5TEEBWWC45FEBQXGIDNMFXHSIDDN7YGSZLTEBQXGIDZN74SA75B
// KRQWWZJANF4CAZDPO7XCA7DPEB4GQZJAMNXXA8JAMNSW67DFOIQGC5TEEBWWC45FEBQXGIDNMFXHSIDDN7YGSZLTEBQXGIDZN74SA75B
