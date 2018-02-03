// Package mtu MTU的常驻探测服务。
// 结合对端能够处理的数据量（MSS）计算，取两者中的低值。
// 用于dcp服务的数据片大小通知。
package pmtu

///////////////////////////////////
/// MTU 值参考
/// 1) 基础值。也为初始轻启动的值。
///    576 （x-44 = 532/IPv4，x-64 = 512/IPv6）
/// 2) 基础值2。IPv6默认包大小。
///    1280（x-64 = 1216/IPv6）
/// 3) PPPoE链路。
///    1492（x-44 = 1448/IPv4，x-64 = 1428/IPv6）
/// 4) 常用以太网通路。
///    1500（x-44 = 1456/IPv4，x-64 = 1436/IPv6）
/// 15) 超标指示。
/// 	由头部后4字节指定大小，常用于超高速网络。
/// 注记：
/// - 此为一个独立的服务，每隔一段时间（10～20分钟）重复执行。
/// - 对于超高速网络，通常传输能力已知，无需测试，仅简单配置即可。
/// - 通告仅在初始联系或MTU发生变化时才产生。
///////////////////////////////////////////////////////////////////////////////

import (
	"encoding/binary"
	"io"
)

// MTU 基本值设置。
const (
	MTUBase     = 1  // 基础值
	MTUBaseIPv6 = 2  // IPv6 基础值
	MTUPPPoE    = 3  // PPPoE 拨号带宽
	MTUEther    = 4  // 普通网卡
	MTUFull64k  = 14 // 本地64k窗口
)

//
// MTU 等级值定义。
//
var mtuValue = map[byte]int{
	0:  0,     // 协商保持
	1:  576,   // 基础值，起始轻启动
	2:  1280,  // 基础值2，IPv6默认包大小
	3:  1492,  // PPPoE链路大小
	4:  1500,  // 以太网络
	14: 65535, // 本地最大窗口
	15: -1,    // 扩展
}

//
// MTU 等级索引。
// mtuValue 的键值反转。用于探测值对应。
//
var mtuIndex = map[int]byte{
	0:     0,  // 保持不变
	576:   1,  // 基础值
	1280:  2,  // 基础值2
	1492:  3,  // PPPoE链路大小
	1500:  4,  // 以太网络
	65535: 14, // 本地最大窗口
}

// MTU 大小。
func (h *header) MTUSize() int {
	if h.Ext2>>4 == 0xf {
		return int(h.mtuSz)
	}
	return mtuValue[h.Ext2>>4]
}

// 设置MTU大小。
func (h *header) SetMTU(sz int) error {
	if sz > mtuLimit {
		return errMTULimit
	}
	i, ok := mtuIndex[sz]
	if !ok {
		h.mtuSz = uint32(sz)
		i = 0xf
	}
	h.Ext2 = h.Ext2&0xf | i<<4
	return nil
}

//
// 返回可携带的有效负载大小。
// 注：减去头部固定长度和可变的长度部分。
//
func (h *header) DataSize() int {
	hd := headUDP + headBase
	if h.Ext2&0xf == 0xf {
		hd += mtuExtra
	}
	return h.MTUSize() - hd
}

// 读取自定义MTU配置。
func (h *header) mtuCustom(r io.Reader) error {
	var buf [mtuExtra]byte
	if n, err := io.ReadFull(r, buf[:]); n != mtuExtra {
		return err
	}
	h.mtuSz = binary.BigEndian.Uint32(buf[:])
	return nil
}
