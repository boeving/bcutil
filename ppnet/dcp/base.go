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

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

const (
	headBase = 16
	mtuExtra = 4
)

var (
	errExt2 = errors.New("value overflow, must be between 0-15")
)

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
	BYE                  // 断开连系
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
func (h *header) MTUSize() uint32 {
	if h.Ext2&0xf == 0xf {
		return h.mtuSz
	}
	return mtuValue[h.Ext2&0xf]
}

// RPZ 扩展大小。
func (h *header) RPZSize() int {
	return int(h.Ext2 >> 4)
}

// 扩展区MTU设置。
func (h *header) SetMTU(i int, cst uint32) error {
	if i > 0xf || i < 0 {
		return errExt2
	}
	h.Ext2 = h.Ext2&0xf0 | byte(i)
	if i == 0xf {
		h.mtuSz = cst
	}
	return nil
}

// 扩展区RPZ值设置。
func (h *header) SetRPZ(v int) error {
	if v > 0xf || v < 0 {
		return errExt2
	}
	h.Ext2 = h.Ext2&0xf | byte(v)<<4
	return nil
}

// 解码头部数据。
func (h *header) Decode(r io.Reader) error {
	var buf [headBase]byte
	n, err := io.ReadFull(r, buf[:])
	if n != headBase {
		return err
	}
	h.SID = binary.BigEndian.Uint16(buf[0:2])
	h.Seq = binary.BigEndian.Uint16(buf[2:4])
	h.RID = binary.BigEndian.Uint16(buf[4:6])
	h.Ack = binary.BigEndian.Uint16(buf[6:8])
	h.Ext2 = buf[8]
	h.Flag = flag(buf[9])
	h.AckDst = buf[10]
	h.SndDst = buf[11]
	h.Sess = binary.BigEndian.Uint32(buf[12:16])

	// MTU置位才有效！
	if h.Flag.MTU() && h.Ext2&0xf == 0xf {
		err = h.mtuCustom(r)
	}
	return err
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

// 编码头部数据。
func (h *header) Encode() []byte {
	var buf [headBase + mtuExtra]byte

	binary.BigEndian.PutUint16(buf[0:2], h.SID)
	binary.BigEndian.PutUint16(buf[2:4], h.Seq)
	binary.BigEndian.PutUint16(buf[4:6], h.RID)
	binary.BigEndian.PutUint16(buf[6:8], h.Ack)
	buf[8] = h.Ext2
	buf[9] = byte(h.Flag)
	buf[10] = h.AckDst
	buf[11] = h.SndDst
	binary.BigEndian.PutUint32(buf[12:16], h.Sess)

	// MTU置位才有效！
	if h.Flag.MTU() && h.Ext2&0xf == 0xf {
		binary.BigEndian.PutUint32(buf[16:20], h.mtuSz)
		return buf[:]
	}
	return buf[:headBase]
}

//
// 数据报。
//
type packet struct {
	Header  header
	Payload []byte
}

//
// 获取资源请求的数据ID。
// 返回值实际上是一个16位大小的整数，用于数据体标识。
// 这主要由客户端的实现调用。
//
// res 为资源标识，可能是一个哈希序列，或一个URI定位。
//
func dataID(res []byte) int {
	//
}

type DCPAddr struct {
	addr net.UDPAddr
}

func (a *DCPAddr) Network() string {
	return a.addr.Network()
}

func (a *DCPAddr) String() string {
	return a.addr.String()
}

//
// ResolveDCPAddr 解析生成地址。
// network兼容 "dcp", "dcp4", "dcp6" 和 "udp", "udp4", "udp6"。
//
// 目前返回的实际上是一个UDP地址。
//
func ResolveDCPAddr(network, address string) (*DCPAddr, error) {
	switch network {
	case "dcp":
		network = "udp"
	case "dcp4":
		network = "udp4"
	case "dcp6":
		network = "udp6"
	}
	addr, err := net.ResolveUDPAddr(network, address)

	return &DCPAddr{addr}, err
}

//
// Options DCP服务选项。
//
type Options struct {
}

//
// Thread 处理进程。
// 负责一对端点连系的处理，接收客户端/服务器实例的注册。
//
type Thread struct {
	//
}

//
// RegisterClient 注册客户端应用。
//
func (th *Thread) RegisterClient(c Client) {
	//
}

//
// RegisterServer 注册服务端应用。
//
func (th *Thread) RegisterServer(s Server) {
	//
}

//
// Serve 启动服务。
//
func (th *Thread) Serve(opt *Options) {
	//
}
