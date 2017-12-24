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
//	| RPZ-  | MTU-  |R|R|S|R|B|R|B|E|               |               |
//	| Extra | ANN/  |E|S|E|S|Y|T|E|N|  ACK distance | Send distance |
//	| Size  | ACK   |Q|P|S|T|E|P|G|D|               |               |
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
	errExt2    = errors.New("value overflow, must be between 0-15")
	errNetwork = errors.New("bad network name between two DCPAddr")
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
	RSP                  // 响应（response）
	REQ                  // 请求（request）
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

func (f flag) RSP() bool {
	return f&RSP != 0
}

func (f flag) REQ() bool {
	return f&REQ != 0
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

	if h.Ext2&0xf == 0xf {
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

	if h.Ext2&0xf == 0xf {
		binary.BigEndian.PutUint32(buf[16:20], h.mtuSz)
		return buf[:]
	}
	return buf[:headBase]
}

//
// 数据报。
//
type packet struct {
	Header  *header
	Payload []byte
}

func getPacket(conn *net.UDPConn) (*packet, net.Addr, error) {

}

func (p *packet) Bytes() []byte {
	//
}

func (p *packet) SendTo(conn *net.UDPConn) error {
	//
}

//
// Receiver 接收器接口。
// 它由发出请求的客户端应用实现。获取数据并处理。
//
type Receiver interface {
	io.Writer
}

//
// Sender 发送器接口。
// 由提供数据服务的应用实现，
// 返回的读取器读取完毕时表示数据体结束。
//
type Sender interface {
	// res 为客户端请求资源的标识。
	// addr 为远端地址。
	NewReader(res []byte, addr net.Addr) (io.Reader, error)
}

//
// DCPAddr 地址封装。
//
type DCPAddr struct {
	net  string
	addr *net.UDPAddr
}

func (d *DCPAddr) Network() string {
	return d.addr.Network()
}

func (d *DCPAddr) String() string {
	return d.addr.String()
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

	return &DCPAddr{network, addr}, err
}
