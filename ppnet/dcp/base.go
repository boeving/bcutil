// Package dcp 数据报控制协议（datagram Control Protocol）。
//
// 基于UDP实现一个「完整数据」传送的逻辑，因此类似于文件，有边界。
// 主要用于P2P的数据传输。
//
// 结构：
//  0                              15                              31
//  +-------------------------------+-------------------------------+
//  |          Data ID #SND         |          Data ID #RCV         |
//  +-------------------------------+-------------------------------+
//  |                        Sequence number                        |
//  +---------------------------------------------------------------+
//  |                      Acknowledgment number                    |
//  +-------------------------------+-------------------------------+
//  |B|E|R|B|R|S|        ...        |              |                |
//  |Y|N|T|E|E|E|        ...        | ACK distance |  Send distance |
//  |E|D|P|G|Q|S|       (2+8)       |      (6)     |      (10)      |
//  +-------------------------------+-------------------------------+
//  |                      Session verify code                      |
//  +---------------------------------------------------------------+
// 	|                       ...... (Payload)                        |
//
//	长度：20 字节
//	端口：UDP报头指定（4）
//
// 说明详见 header.txt, design.txt。
//
package dcp

///////////////
/// MTU 值参考
/// 1) 基础值。也为初始轻启动的值。
///    576 （x-44 = 532/IPv4，x-64 = 512/IPv6）
/// 2) 基础值IPv6。IPv6默认包大小。
///    1280（x-64 = 1216/IPv6）
/// 3) PPPoE链路。
///    1492（x-44 = 1448/IPv4，x-64 = 1428/IPv6）
/// 4) 常用以太网通路。
///    1500（x-44 = 1456/IPv4，x-64 = 1436/IPv6）
///////////////////////////////////////////////////////////////////////////////

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/goes"
)

const (
	headIP      = 20                         // IP 报头长度
	headUDP     = 8                          // UDP报文头部长
	headDCP     = 20                         // DCP头部长
	headAll     = headDCP + headUDP + headIP // 头部总长（除IP报头）
	mtuBase     = 576                        // 基础MTU值
	mtuBaseIPv6 = 1280                       // IPv6 MTU基础值
)

var (
	errRpzSize  = errors.New("value overflow, must be between 0-15")
	errNetwork  = errors.New("bad network name between two DCPAddr")
	errOverflow = errors.New("exceeded the number of resource queries")
	errZero     = errors.New("no data for Query")
	errNoRAddr  = errors.New("no remote address")
	errDistance = errors.New("the distance value is out of range")
	errHeadSize = errors.New("header length is not enough")
)

//
// 路径MTU全局共享。
// 该值由所有子服务共享，由PMTU探测服务设置。
//
var mtuGlobal = mtuBase
var mtuShare sync.Mutex

//
// PathMTU 获取全局路径MTU大小。
//
func PathMTU() int {
	mtuShare.Lock()
	defer mtuShare.Unlock()
	return mtuGlobal
}

//
// SetPMTU 设置全局路径MTU共享。
//
func SetPMTU(size int) {
	mtuShare.Lock()
	mtuGlobal = size
	mtuShare.Unlock()
}

//
// 头部标记。
//
type flag uint8

//
// 标记常量定义。
//
const (
	_   flag = 1 << iota
	_        // ...
	SES      // 会话申请/更新
	REQ      // 请求
	BEG      // 分组开始
	RTP      // 请求重发
	END      // 分组结束
	BYE      // 断开连系
)

func (f flag) SES() bool {
	return f&SES != 0
}

func (f flag) REQ() bool {
	return f&REQ != 0
}

func (f flag) RTP() bool {
	return f&RTP != 0
}

func (f flag) BYE() bool {
	return f&BYE != 0
}

func (f flag) BEG() bool {
	return f&BEG != 0
}

func (f flag) END() bool {
	return f&END != 0
}

func (f flag) BEGEND() bool {
	return f&BEG != 0 && f&END != 0
}

func (f *flag) Set(v flag) {
	*f |= v
}

//
// 头部结构。
// 实现约定的解析和设置。
//
type header struct {
	flag            // 标志区（8）
	SID, RID uint16 // 发送/接收数据ID
	Seq, Ack uint32 // 序列号，确认号
	None     byte   // 保留未用
	AckDst   uint   // ACK distance，确认距离
	SndDst   uint   // Send distance，发送距离
	Sess     uint32 // Session verify code
}

//
// 解码头部数据。
//
func (h *header) Decode(buf []byte) error {
	if len(buf) != headDCP {
		return errHeadSize
	}
	// binary.BigEndian.Uint16(buf[x:x+2])
	h.SID = uint16(buf[0])<<8 | uint16(buf[1])
	h.RID = uint16(buf[2])<<8 | uint16(buf[3])
	h.Seq = binary.BigEndian.Uint32(buf[4:8])
	h.Ack = binary.BigEndian.Uint32(buf[8:12])

	h.flag = flag(buf[12])
	h.None = buf[13]
	h.AckDst = uint(buf[14]) >> 2
	h.SndDst = uint(buf[15]) | uint(buf[14]&3)<<8
	h.Sess = binary.BigEndian.Uint32(buf[16:20])

	return nil
}

//
// 编码头部数据。
//
func (h *header) Encode() ([]byte, error) {
	if h.AckDst > 0x3f || h.SndDst > 0x3ff {
		return nil, errDistance
	}
	var buf [headDCP]byte

	binary.BigEndian.PutUint16(buf[0:2], h.SID)
	binary.BigEndian.PutUint16(buf[2:4], h.RID)
	binary.BigEndian.PutUint32(buf[4:8], h.Seq)
	binary.BigEndian.PutUint32(buf[8:12], h.Ack)

	buf[12] = byte(h.flag)
	buf[13] = h.None
	buf[14] = byte(h.AckDst)<<2 | byte(h.SndDst>>8)
	buf[15] = byte(h.SndDst & 0xff)
	binary.BigEndian.PutUint32(buf[16:20], h.Sess)

	return buf[:], nil
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
	b, err := p.Encode()
	if err != nil {
		panic(err)
	}
	return append(b, p.Data...)
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

//
// 读取构造数据报实例。
//
func (r *connReader) Receive() (*packet, net.Addr, error) {
	buf := make([]byte, headDCP)

	n, addr, err := r.Conn.ReadFrom(buf)
	if err != nil {
		return nil, addr, err
	}
	if n != headDCP {
		return nil, addr, errHeadSize
	}
	h := new(header)
	h.Decode(buf)
	b, err := ioutil.ReadAll(r.Conn)

	if err != nil {
		return nil, addr, err
	}
	return &packet{h, b}, addr, nil
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

//
// 写入目标数据报实例。
//
func (w *connWriter) Send(p packet) (int, error) {
	return w.Conn.Write(p.Bytes())
}

//
// 简单读取服务。
// 成功读取后将数据报发送到service。
// 外部可通过Stop.Exit()结束服务。
// 注：
// 仅用于直接拨号连系（Dial）时的读取转发，
// istener会在Accept时自己接收数据转发。
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
// 服务启动（阻塞）。
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
			go s.Serv.Post(*pack)
		}
	}
}

//
// Receiver 接收器接口。
// 它由发出请求的客户端应用实现。接收响应数据并处理。
//
type Receiver interface {
	io.Writer
	io.Closer
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
	rdsrv        *servReader // 简单读取服务（Dial需要）
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
		begid: uint16(rand.Intn(xLimit16 - 1)),
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
