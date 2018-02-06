package ppnet

//////////////////////////////////////////////////
/// DCP 数据报控制协议（Datagram Control Protocol）
///
/// 基于UDP实现一个「完整数据」传送的逻辑，因此类似于文件，有边界。
/// 主要用于P2P的数据传输。
///
/// 结构：
/// 0                              15                              31
/// +-------------------------------+-------------------------------+
/// |          Data ID #SND         |          Data ID #RCV         |
/// +-------------------------------+-------------------------------+
/// |                        Sequence number                        |
/// +---------------------------------------------------------------+
/// |                      Acknowledgment number                    |
/// +-------------------------------+-------------------------------+
/// |B|E|R|B|Q|R|S|.|      ...      |              |                |
/// |Y|N|T|E|E|E|E|.|      ...      | ACK distance |  Send distance |
/// |E|D|P|G|R|Q|S|.|      (8)      |      (6)     |      (10)      |
/// +-------------------------------+-------------------------------+
/// |                      Session verify code                      |
/// +---------------------------------------------------------------+
/// |                       ...... (Payload)                        |
///
///	长度：20 字节
///	端口：UDP报头指定（4）
///
/// 说明详见 header.txt, design.txt。
///
///////////////
/// MTU 值参考
///
/// 1) 基础值。也为初始轻启动的值。
///    576 （x-48 = 528/IPv4，x-68 = 508/IPv6）
/// 2) 基础值IPv6。IPv6默认包大小。
///    1280（x-68 = 1212/IPv6）
/// 3) PPPoE链路。
///    1492（x-48 = 1444/IPv4，x-68 = 1424/IPv6）
/// 4) 常用以太网通路。
///    1500（x-48 = 1452/IPv4，x-68 = 1432/IPv6）
///
///////////////////////////////////////////////////////////////////////////////

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"sync"

	"github.com/qchen-zh/pputil/goes"
	"golang.org/x/tools/container/intsets"
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
// PayloadSize 计算分组有效负载尺寸。
//
func PayloadSize() int {
	return PathMTU() - headAll
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
	SES      // 会话申请/更新
	REQ      // 资源请求
	QER      // 请求确认
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

func (f flag) QER() bool {
	return f&QER != 0
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

func (p packet) Size() int {
	return len(p.Data)
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
// 注记：
// 仅对读取接口实现Close方法。
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
// 注记：Close由connReader实现。
//
type connWriter struct {
	Raddr net.Addr
	Conn  *net.UDPConn
}

//
// 写入目标数据报实例。
//
func (w *connWriter) Send(p packet) (int, error) {
	return w.Conn.WriteTo(p.Bytes(), w.Raddr)
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
	read *connReader
	post func(*packet)
	stop *goes.Stop
}

func newServReader(conn *net.UDPConn, post func(*packet)) *servReader {
	return &servReader{
		read: &connReader{conn},
		post: post,
		stop: goes.NewStop(),
	}
}

//
// 服务启动（阻塞）。
//
func (s *servReader) Serve() {
	for {
		select {
		case <-s.stop.C:
			return
		default:
			pack, _, err := s.read.Receive()
			if err != nil {
				break
			}
			go s.post(pack)
		}
	}
}

func (s *servReader) Exit() {
	s.stop.Exit()
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
	GetReader(res []byte, addr net.Addr) (io.Reader, error)
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
	servs        *service    // 接收服务（分配）
	sends        *xSender    // 总发送器
	stop         *goes.Stop  // 结束通知
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
		servs: srv,
		sends: newXSender(),
		rdsrv: newServReader(udpc, srv.Post),
	}

	go c.servs.Start() // 接收服务启动
	go c.rdsrv.Serve() // 网络读取服务启动

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
	c.servs.Resp = resp
}

//
// Query 资源查询。
// 最多同时处理64k的目标查询，超出会返回errOverflow错误。
// 目标资源不可为空，否则返回一个errZero错误。
//
//  res  资源的标识。
//  rec  外部接收器接口的实现。
//
func (c *Contact) Query(res []byte, rec Receiver) error {
	if len(res) == 0 {
		return errZero
	}
	// ...
}

//
// Bye 主动断开。
// 无论数据是否传递完毕，都会结束发送或接收。
// 对端可能是一个服务器，也可能是一个普通的客户端。
//
func (c *Contact) Bye() {
	if c.rdsrv != nil {
		c.rdsrv.Exit()
	}
	c.stop.Exit()
}

//
// 简单工具集。
///////////////////////////////////////////////////////////////////////////////

//
// 返回序列号增量回绕值。
// 支持负增量值。排除xLimit32值本身。
//
func roundPlus(x uint32, n int) uint32 {
	v := int64(x) + int64(n) + xLimit32
	return uint32(v % xLimit32)
}

//
// 返回2字节（16位）增量回绕值。
// 注：排除xLimit16值本身。
//
func roundPlus2(x uint16, n int) uint16 {
	return uint16((int(x) + n + xLimit16) % xLimit16)
}

//
// 支持回绕的间距计算。
// 环回范围为全局常量xLimit32。
//
func roundSpacing(beg, end uint32) int {
	if end >= beg {
		return int(end - beg)
	}
	// 不计xLimit32本身
	return int(xLimit32 - (beg - end))
}

//
// 支持回绕的间距计算。
// 环回范围为全局常量xLimit16。
//
func roundSpacing2(beg, end uint16) int {
	if end >= beg {
		return int(end - beg)
	}
	return int(xLimit16 - (beg - end))
}

//
// 支持回绕的起点计算。
// 注意dist的值应当在uint32范围内。
//
func roundBegin(end uint32, dist int) uint32 {
	d := uint32(dist)
	if end > d {
		return end - d
	}
	return xLimit32 - (d - end)
}

//
// 限定计数器。
// 对特定键计数，到达和超过目标限度后返回真。
// 不含限度值本身（如：3，返回2个true）。
// 如果目标键改变，计数起始重置为零。
//
// max 为计数最大值（应为正值）。
// step 为递增步进值。
//
// 返回的函数用于递增和测试，参数为递增计数键。
//
func limitCounter(max, step int) func(int) bool {
	var cnt, key int

	return func(k int) bool {
		if k != key {
			key = k
			cnt = 0
		}
		cnt += step
		return cnt >= max
	}
}

//
// 切分数据片为指定大小的子片。
//
func pieces(data []byte, size int) [][]byte {
	n := len(data) / size
	buf := make([][]byte, 0, n+1)

	for i := 0; i < n; i++ {
		x := i * size
		buf = append(buf, data[x:x+size])
	}
	if len(data)%size > 0 {
		buf = append(buf, data[n*size:])
	}
	return buf
}

//
// 序列号有序压入。
// 用于有回绕逻辑的序列号和确认号的有序排列。
// 注意，首个序列号必须已经存在，且在逻辑上为最前。
// 注记：
// 与首个序列号/确认号比较距离即可。
//
func orderPush(buf []uint32, seq uint32) []uint32 {
	i := len(buf) - 1
	v := roundSpacing(buf[0], seq)

	// 先扩展
	if cap(buf) == len(buf) {
		buf = append(buf, 0)
	}
	// 逆向：后到的确认号通常靠后。
	for ; i >= 0; i-- {
		if v > roundSpacing(buf[0], buf[i]) {
			break
		}
	}
	i++
	// 后移一格
	// 最后的0被有效值覆盖。
	copy(buf[i+1:], buf[i:])
	buf[i] = seq

	return buf
}

//
// 序列号队列。
// 先进先出的逻辑，但后添加的重复值无效。
// 处理不连续序列号/确认号的排列问题。
// 注：
// 也可用于确认号的处理，同为回绕逻辑。
//
type seqQueue struct {
	buf []uint32       // 序列号集
	set intsets.Sparse // 重复排除
}

//
// 新建一个队列。
// first应当为逻辑上的首个序列号。
//
func newSeqQueue(first uint32) *seqQueue {
	sq := seqQueue{
		buf: []uint32{first},
	}
	sq.set.Insert(int(first))
	return &sq
}

//
// 添加一个序列号。
// 注意，新的序列号必然在首个序列号之后。
// 成功添加后返回true，否则返回false（重复）。
//
func (q *seqQueue) Push(seq uint32) bool {
	if !q.set.Insert(int(seq)) {
		return false
	}
	q.buf = orderPush(q.buf, seq)
	return true
}

//
// 进度清理。
// 移除进度线之前的历史（不含进度本身）。
// 返回移除的条目数量。
// 注记：
// 原序列即已有序，从头开始检查。
//
func (q *seqQueue) Clean(seq uint32) int {
	if !q.set.Has(int(seq)) {
		return 0
	}
	i := 0
	for ; i < len(q.buf); i++ {
		if q.buf[i] == seq {
			break
		}
		q.set.Remove(int(q.buf[i]))
	}
	q.buf = q.buf[i:]
	return i
}

//
// 返回队列大小。
//
func (q *seqQueue) Size() int {
	return len(q.buf)
}

//
// 返回内部有序队列。
// 外部不应当修改返回的队列。
// 返回的队列仅在新的Add调用之前有效。
// 注：
// 主要用于外部迭代读取。
//
func (q *seqQueue) Queue() []uint32 {
	return q.buf
}
