// Package dcp 数据报控制协议（datagram Control Protocol）。
//
// 基于UDP实现一个「完整数据」传送的逻辑，因此类似于文件，有边界。
// 主要用于P2P的数据传输。
//
// 结构：
//	0                              15                              31
//	+-------------------------------+-------------------------------+
//	|          Data ID #SND         |        Sequence number        |
//	+-------------------------------+-------------------------------+
//	|          Data ID #ACk         |     Acknowledgment number     |
//	+-------------------------------+-------------------------------+
//	| MTU-  |  ...  |.|R|S|R|B|R|B|E|              |                |
//	| ANN/  |  ...  |.|E|E|S|Y|T|E|N| ACK distance |  Send distance |
//	| ACK   |  (4)  |.|Q|S|T|E|P|G|D|      (6)     |     (10)       |
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
	"time"

	"github.com/qchen-zh/pputil/goes"
)

const (
	headUDP  = 8         // UDP报文头部长
	headBase = 16        // 基础长度
	mtuExtra = 4         // MTU扩展长度
	mtuLimit = 1<<32 - 1 // MTU定制最大长度
)

var (
	errMTULimit = errors.New("value overflow, must be 32 bits")
	errNetwork  = errors.New("bad network name between two DCPAddr")
	errOverflow = errors.New("exceeded the number of resource queries")
	errZero     = errors.New("no data for Query")
	errNoRAddr  = errors.New("no remote address")
	errDistance = errors.New("the distance value is out of range")
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

//
// 头部结构。
// 实现约定的解析和设置。
//
type header struct {
	SID, Seq uint16 // 发送数据ID，序列号
	RID, Ack uint16 // 接受数据ID，确认号
	Ext2     byte   // 扩展区（4+4）
	Flag     flag   // 标志区（8）
	AckDst   uint   // ACK distance，确认距离
	SndDst   uint   // Send distance，发送距离
	Sess     uint32 // Session verify code
	mtuSz    uint32 // MTU Custom size
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

// 解码头部数据。
func (h *header) Decode(r io.Reader) error {
	var buf [headBase]byte
	n, err := io.ReadFull(r, buf[:])
	if n != headBase {
		return err
	}
	// binary.BigEndian.Uint16(buf[x:x+2])
	h.SID = uint16(buf[0])<<8 | uint16(buf[1])
	h.Seq = uint16(buf[2])<<8 | uint16(buf[3])
	h.RID = uint16(buf[4])<<8 | uint16(buf[5])
	h.Ack = uint16(buf[6])<<8 | uint16(buf[7])

	h.Ext2 = buf[8]
	h.Flag = flag(buf[9])
	h.AckDst = uint(buf[10]) >> 2
	h.SndDst = uint(buf[11]) | uint(buf[10]&3)<<8
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
func (h *header) Encode() ([]byte, error) {
	if h.AckDst > 0x3f || h.SndDst > 0x3ff {
		return nil, errDistance
	}
	var buf [headBase + mtuExtra]byte

	binary.BigEndian.PutUint16(buf[0:2], h.SID)
	binary.BigEndian.PutUint16(buf[2:4], h.Seq)
	binary.BigEndian.PutUint16(buf[4:6], h.RID)
	binary.BigEndian.PutUint16(buf[6:8], h.Ack)

	buf[8] = h.Ext2
	buf[9] = byte(h.Flag)
	buf[10] = byte(h.AckDst)<<2 | byte(h.SndDst>>8)
	buf[11] = byte(h.SndDst & 0xff)
	binary.BigEndian.PutUint32(buf[12:16], h.Sess)

	if h.Ext2&0xf == 0xf {
		binary.BigEndian.PutUint32(buf[16:20], h.mtuSz)
		return buf[:], nil
	}
	return buf[:headBase], nil
}

//
// 数据报。
// 合并报头的方法便于使用。
//
type packet struct {
	*header
	Data []byte
}

//
// Bytes 编码为字节序列。
// 如果出错返回nil，同时记录日志。这通常很少发生。
//
func (p packet) Bytes() []byte {
	//
}

//
// datagram 数据报处理器。
// 面对网络套接字的简单接收和发送处理。
//
type datagram struct {
	connReader          // 读取器
	connWriter          // 写入器
	Laddr      net.Addr // 本地地址
}

//
// 网络连接读取器。
//
type connReader struct {
	Conn *net.UDPConn
}

func (r *connReader) Receive() (packet, net.Addr, error) {
	//
}

func (r *connReader) Close() error {
	return r.Conn.Close()
}

//
// 网络连接写入器。
//
type connWriter struct {
	Raddr net.Addr
	Conn  *net.UDPConn
}

func (w *connWriter) Send(data packet) (int, error) {
	//
}

//
// 简单读取服务。
// 成功读取后将数据报（packet）发送给DCP服务器。
// 外部可通过Stop.Exit()结束服务。
//
type servReader struct {
	Read *connReader
	Serv *service
	Stop *goes.Stop
}

func newServReader(conn *net.UDPConn, srv *service) *servReader {
	return &servReader{
		Read: &connReader{conn},
		Serv: srv,
		Stop: goes.NewStop(),
	}
}

//
// 服务启动。
// 应当在一个Go程中单独运行。
//
func (s *servReader) Serve() {
	for {
		pack, _, err := s.Read.Receive()
		select {
		case <-s.Stop.C:
			return
		default:
		}
		if err == nil {
			go s.Serv.Post(pack)
		}
	}
}

//
// Receiver 接收器接口。
// 它由发出请求的客户端应用实现。接收响应数据并处理。
//
type Receiver interface {
	io.Writer
}

//
// Responser 响应器接口。
// 响应对端的资源请求，由提供数据服务的应用实现。
// 返回的读取器读取完毕表示数据体结束。
//
type Responser interface {
	// res 为客户端请求资源的标识。
	// addr 为远端地址。
	GetReader(res []byte) (io.Reader, error)
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

//////////////
/// 实现注记
/// ========
/// 客户端：
/// - 数据ID生成，优先投递数据体首个分组（有序）；
/// - 启动并发的发送服务器servSend（如果还需要）；
/// - 创建并缓存接收服务器recvServ，用于对对端响应的接收；
/// - 视情况添加响应服务（若snd有效）；
///
/// 服务端：
/// - 不可直接读取conn连接（由Listener代理调度），仅用于写；
/// - 外部可像使用客户端一样向对端发送资源请求（Query...）；
/// - resp 成员必定存在（而客户端为可选）；
///
///////////////////////////////////////////////////////////////////////////////

//
// Contact 4元组两端连系。
// DCP/P2P语境下的 Connection。
//
type Contact struct {
	laddr, raddr net.Addr    // 4元组
	serv         *service    // DCP内部服务
	alive        time.Time   // 最近活跃（收/发）
	begid        uint16      // 起始数据ID（交互期）
	rdsrv        *servReader // 读取服务器，可选
}

//
// Dial 拨号连系。
// 可以传入一个指定的本地接收地址，否则系统自动配置。
// 返回的Contact实例处理一个4元组两端连系。
// 通常由客户端逻辑调用。
//
func Dial(laddr, raddr *DCPAddr) (*Contact, error) {
	n1 := laddr.net
	n2 := raddr.net
	if n1 != n2 {
		return nil, errNetwork
	}
	udpc, err := net.DialUDP(n1, laddr.addr, raddr.addr)
	if err != nil {
		return nil, err
	}
	srv := newService(&connWriter{raddr.addr, udpc}, nil)

	c := Contact{
		laddr: laddr.addr,
		raddr: raddr.addr,
		serv:  srv,
		alive: time.Now(),
		rdsrv: newServReader(udpc, srv),
	}

	go c.serv.Start()  // DCP服务启动
	go c.rdsrv.Serve() // 读取服务启动

	return &c, nil
}

//
// String 客户端的字符串表示。
// 格式：本地地址|对端地址
// 主要用于端点连接池里的索引和管理。
//
func (c *Contact) String() string {
	return c.laddr.String() +
		"|" +
		c.raddr.String()
}

//
// LocalAddr 返回本地端地址。
//
func (c *Contact) LocalAddr() net.Addr {
	return c.laddr
}

//
// RemoteAddr 返回本地端地址。
//
func (c *Contact) RemoteAddr() net.Addr {
	return c.raddr
}

//
// Register 设置响应服务。
// 非并发安全，服务端应当在Listener:Accept返回的最初时设置。
// 如果客户端（Dial者）也需提供响应服务，此注册也有效。
//
// 注记：
// 单独的注册接口提供一种灵活性。如不同对端不同对待。
//
func (c *Contact) Register(resp Responser) {
	c.serv.Resp = resp
}

//
// Query 向服务端查询数据。
// 最多同时处理64k的目标查询，超出会返回errOverflow错误。
//
//  res  目标数据的标识。
//  rec  外部接收器接口的实现。
//
func (c *Contact) Query(res []byte, rec Receiver) error {
	if len(res) == 0 {
		return errZero
	}
	// ...
}

//
// 字节序列名称。
// 提取最多32字节数据，返回16进制表示串。
//
/*
func (c *Contact) bytesName(res []byte) string {
	n := len(res)
	if n > 32 {
		n = 32
	}
	return fmt.Sprintf("%x", res[:n])
}
*/

//
// StartID 设置起始数据ID。
// 通常在会话申请或更新后被调用。其值用于下一个交互期。
//
func (c *Contact) StartID(rnd uint16) {
	c.begid = rnd
}

//
// Bye 断开连系。
// 无论数据是否传递完毕，都会结发送或接收。
// 未完成数据传输的中途结束会返回一个错误，记录了一些基本信息。
//
// 对端可能是一个服务器，也可能是一个普通的客户端。
//
func (c *Contact) Bye() error {
	if c.rdsrv != nil {
		c.rdsrv.Stop.Exit()
	}
	return c.serv.Exit()
}
