// Package dcp 数据报控制协议（Datagram Control Protocol）。
//
// 基于UDP实现一个「完整数据」传送的逻辑，因此类似于文件，有边界。
// 主要用于P2P的数据传输。
//
// 结构：
//	0                              15                              31
//	+-------------------------------+-------------------------------+
//	|          Data ID #SND         |        Sequence number        |
//	+-------------------------------+-------------------------------+
//	|          Data ID #RCV         |     Acknowledgment number     |
//	+-------------------------------+-------------------------------+
//	| RPZ-  | MTU-  |R|M|S|R|B|R|B|E|               |               |
//	| Extra | ANN/  |P|T|E|S|Y|T|E|N|  ACK distance | Send distance |
//	| Size  | ACK   |Z|U|S|T|E|P|G|D|               |               |
//	+-------------------------------+-------------------------------+
//	|                      Session verify code                      |
//	+---------------------------------------------------------------+
//	|                   MTU Custom size (optional)                  |
//	+---------------------------------------------------------------+
// 	|                       ...... (Payload)                        |
//
//	长度：16 + 4 可选
//	端口：UDP报头指定（4）
//
// 详见设计说明（design.txt）。
//
package dcp

import "io"

//
// Client 客户端应用。
//
type Client struct {
}

//
// Server 服务器。
//
type Server struct {
}

//
// 头部标记。
//
type flag uint8

//
// 标记常量定义。
//
const (
	END flag = 1 << iota // 分组结束
	BEG                  // 分组开始
	RTP                  // 请求重发
	BYE                  // 中断连系
	RST                  // 发送/会话重置
	SES                  // 会话申请/更新
	MTU                  // MTU通告/确认
	RPZ                  // 重新组包
)

func (f flag) END() bool {
	return f&END != 0
}

func (f flag) BEG() bool {
	return f&BEG != 0
}

func (f flag) RTP() bool {
	return f&RTP != 0
}

func (f flag) BYE() bool {
	return f&BYE != 0
}

func (f flag) RST() bool {
	return f&RST != 0
}

func (f flag) SES() bool {
	return f&SES != 0
}

func (f flag) MTU() bool {
	return f&MTU != 0
}

func (f flag) RPZ() bool {
	return f&RPZ != 0
}

func (f flag) BEGEND() bool {
	return f&BEG != 0 && f&END != 0
}

func (f flag) RSTSES() bool {
	return f&RST != 0 && f&SES != 0
}

func (f *flag) Set(v flag) {
	*f |= v
}

//
// MTU 常用值定义。
//
var mtuValue = map[uint8]uint32{
	0: 0,    // 协商保持
	1: 576,  // 基础值，起始轻启动
	2: 1280, // 基础值2，IPv6默认包大小
	3: 1492, // PPPoE链路大小
	4: 1500, // 以太网络
}

//
// 头部结构。
// 实现约定的解析和设置。
//
type header struct {
	SID, Seq uint16 // 发送数据ID，序列号
	RID, Ack uint16 // 接受数据ID，确认号
	Ext2     byte   // 扩展区（4+4）
	Flag     flag   // 标志区（8）
	AckDst   uint8  // ACK distance，确认距离
	SndDst   uint8  // Send distance，发送距离
	Sess     uint32 // Session verify code
	mtuSz    uint32 // MTU Custom size
}

// MTU 定制大小。
func (h *header) mtuSize() uint32 {
	if h.Ext2&0xf == 0xf {
		return h.mtuSz
	}
	return mtuValue[h.Ext2&0xf]
}

// RPZ 扩展大小。
func (h *header) rpzSize() int {
	return int(h.Ext2 >> 4)
}

// 解码头部数据。
func (h *header) Decode(r io.Reader) {

}

// 编码头部数据。
func (h *header) Encode(w io.Writer) {

}

//
// 数据报。
// 头部和有效载荷。
//
type packet struct {
	h    header
	data []byte
}
