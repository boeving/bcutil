// Package ping 批量主机连接测试包。
//
// 支持批量主机并发测试。连续地址提供者实现Address接口。
// 外部通常设置发送间隔时间为1-2秒（对并发中的单个目标主机）。
//
// ip网络需要特权运行。udp仅支持Linux或Darwin系统。
// $sudo go test
//
package ping

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"syscall"
	"time"

	"github.com/qchen-zh/pputil/goes"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// 简单的基本常量。
const (
	TimeSliceLength  = 8  // 时间值字节数
	ProtocolICMP     = 1  // IPv4 ICMP 协议号
	ProtocolIPv6ICMP = 58 // IPv6 ICMP 协议号
	MaxInt32         = 1<<31 - 1
)

const (
	// 同时发送数据的最多Go程数
	// 注：多个Go程向同一连接写入
	sendThreads = 20

	// 接收消息缓存区大小
	recvBufSize = 512
)

var (
	ipv4Proto = map[string]string{
		"ip":  "ip4:icmp",
		"udp": "udp4",
	}
	ipv6Proto = map[string]string{
		"ip":  "ip6:ipv6-icmp",
		"udp": "udp6",
	}

	errExit = errors.New("the Pinger is exited")
)

//
// Address IP地址生成器。
//
type Address interface {
	IPAddrs(cancel func() bool) <-chan net.Addr
}

//
// Handler 响应处理器。
//
type Handler interface {
	// 正常接收回调。
	//  @id 为发送时的标记值，
	//  @echo 回应的id用于匹配分辨。
	//  @exit 终止ping行为的控制函数。
	Receive(a net.Addr, id int, echo *icmp.Echo, exit func()) error

	// 错误时的回调。
	// 包含连接读取错误和回应非EchoReply类型视为错误。
	// 并发时无法确定目标IP，a为nil。
	// 注意：并发时可能出现大量调用。
	Fail(a net.Addr, err error, exit func())
}

//
// Conn icmp.PacketConn 封装。
//
type Conn struct {
	name string
	conn *icmp.PacketConn
}

//
// Listen 创建监听连接。
// 配合Pinger使用。network实参应与NewPinger的传递值相同。
//  @network 指采用的网络，"ip" 或 "udp"
//  @address 为监听地址，可为IPv4或IPv6。空串视为IPv4地址："0.0.0.0"
//
func Listen(network, address string) (*Conn, error) {
	if network != "ip" && network != "udp" {
		return nil, errors.New(network + " can't be used as ICMP endpoint")
	}
	if address == "" {
		address = "0.0.0.0"
	}
	proto, err := addressProto(network, address)
	if err != nil {
		return nil, err
	}
	conn, err := icmp.ListenPacket(proto, address)

	return &Conn{network, conn}, err
}

//////////
// 发送器
///////////////////////////////////////////////////////////////////////////////

//
// ICMP数据发送器。
//
type sender struct {
	id     int        // Echo ID
	seq    <-chan int // 计数器
	conn   *icmp.PacketConn
	cancel func() bool

	// 附加数据（原起始8字节为时间戳）
	// 需要在任何发送之前设置。
	Extra []byte
}

//
// 创建一个发送器实例。
// @conn 参数为必须，非nil。
//
func newSender(conn *icmp.PacketConn, id int, cancel func() bool) *sender {
	if conn == nil {
		log.Println("conn is nil (valid)")
		return nil
	}
	return &sender{
		id:     id,
		seq:    seqCounts(0, 0xffff, cancel),
		conn:   conn,
		cancel: cancel,
	}
}

//
// Sends 对目标地址连续发送信息包。
//
func (s *sender) Sends(dst net.Addr, t time.Duration, cnt int) error {
	typ, err := icmpType(dst)
	if err != nil {
		return err
	}
	for i := 0; i < cnt; i++ {
		if s.cancel() {
			break
		}
		msg, err := icmpMsg(typ, 0, s.id, <-s.seq, s.Extra)
		if err != nil {
			// log.Println(err)
			continue
		}
		go icmpSend(s.conn, dst, msg, s.cancel)
		time.Sleep(t)
	}
	return nil
}

//
// Send 对目标地址发送一次信息包。
//
func (s *sender) Send(dst net.Addr) error {
	typ, err := icmpType(dst)
	if err != nil {
		return err
	}
	msg, err := icmpMsg(typ, 0, s.id, <-s.seq, s.Extra)
	if err != nil {
		return err
	}
	go icmpSend(s.conn, dst, msg, s.cancel)
	return nil
}

//
// 创建一个序列号生成器。
// 计数为16位回绕，由外部停止循环。
//
func seqCounts(i, max int, cancel func() bool) <-chan int {
	ch := make(chan int)

	go func() {
		for {
			if cancel() {
				break
			}
			ch <- i
			i = (i + 1) % max
		}
		close(ch)
	}()
	return ch
}

//////////
// 接收器
///////////////////////////////////////////////////////////////////////////////

//
// 回应信息包。
// 用于读取ICMP回应信息对外传递。
//
type packet struct {
	Addr  net.Addr
	Bytes []byte
	Err   error
}

//
// receiver ICMP接收器。
//
type receiver struct {
	id   int
	conn *Conn
	stop chan struct{}
	proc Handler // 接收处理器
}

//
// 创建一个接收器实例。
//
func newReceiver(conn *Conn, id int, h Handler, stop chan struct{}) *receiver {
	return &receiver{
		id:   id,
		conn: conn,
		proc: h,
		stop: stop,
	}
}

//
// Serve 启动接收服务。
// 每个接收器只应开启一个服务，阻塞。
//
func (r *receiver) Serve() {
End:
	for {
		select {
		case <-r.stop:
			break End
		default:
		}
		go r.Process(icmpReceive(r.conn.conn))
	}
}

//
// Exit 结束处理。
// 用于传递给用户处理器备用。
//
func (r *receiver) Exit() {
	goes.Close(r.stop)
}

//
// 处理接收的数据包。
// 返回的错误可能是Fail或Receive回调返回的值。
//
func (r *receiver) Process(rd *packet) error {
	if rd == nil {
		return errors.New("package data is nil")
	}
	if rd.Err != nil {
		r.proc.Fail(rd.Addr, rd.Err, r.Exit)
		return rd.Err
	}
	echo, err := replyEchoParse(rd, r.conn.name)
	if err != nil {
		r.proc.Fail(rd.Addr, err, r.Exit)
		return err
	}
	return r.proc.Receive(rd.Addr, r.id, echo, r.Exit)
}

/////////
// 应用
///////////////////////////////////////////////////////////////////////////////

//
// Pinger ICMP ping 处理器。
// 一个实例对应一个随机的消息ID（发送/接收）。
//
type Pinger struct {
	s    *sender       // 消息发送器
	stop chan struct{} // 停止信号量
	end  bool          // 是否已终止
}

//
// NewPinger 创建一个Ping实例。
// caller 回调处理器为必需值。
//
func NewPinger(conn *Conn, caller Handler) (*Pinger, error) {
	if caller == nil {
		return nil, errors.New("must be a handler")
	}
	stop := make(chan struct{})
	rand.Seed(time.Now().UnixNano())
	id := rand.Intn(0xffff)

	// 接收处理器
	rec := newReceiver(conn, id, caller, stop)
	go rec.Serve()

	return &Pinger{
		s:    newSender(conn.conn, id, goes.Canceller(stop)),
		stop: stop,
	}, nil
}

//
// ExtraData 设置消息包附加数据。
// 该设置需要在实际调用 Ping 系列之前执行。
//
func (p *Pinger) ExtraData(data []byte) {
	p.s.Extra = data
}

//
// Exit 结束处理。
// 可能由外部处理操作触发，或用户直接调用。
//
// 结束后的实例不能再次使用。
//
func (p *Pinger) Exit() {
	goes.Close(p.stop)
	p.end = true
}

//
// Ping 单次ping。
// 如果需要出错后重试，可在处理器的Fail中再次调用本函数。
//
func (p *Pinger) Ping(dst net.Addr) error {
	if p.end {
		return errExit
	}
	return p.s.Send(dst)
}

//
// PingLoop 向单个地址循环ping。
// 需要注册消息处理器，否则直接退出。
// 	@t 循环间隔时间
// 	@cnt 循环次数，-1表示无限
//
func (p *Pinger) PingLoop(dst net.Addr, t time.Duration, cnt int) error {
	if p.end {
		return errExit
	}
	if cnt == 0 {
		return nil
	}
	if cnt > 0 {
		return p.s.Sends(dst, t, cnt)
	}
	cancel := goes.Canceller(p.stop)
	for {
		if cancel() {
			break
		}
		p.s.Sends(dst, t, MaxInt32)
	}
	return nil
}

//
// Pings 对地址集批量ping（单次）。
// 失败的ping地址可以通过Fail()获得。
//
func (p *Pinger) Pings(as Address) error {
	if p.end {
		return errExit
	}
	pingSends(
		as.IPAddrs(goes.Canceller(p.stop)),
		func(a net.Addr) {
			p.s.Send(a)
		},
	)
	return nil
}

//
// PingsLoop 对地址集批量ping（多次）。
// 每一个地址都会按照时间间隔尝试多次，cnt不支持无限。
//
func (p *Pinger) PingsLoop(as Address, t time.Duration, cnt int) error {
	if p.end {
		return errExit
	}
	pingSends(
		as.IPAddrs(goes.Canceller(p.stop)),
		func(a net.Addr) {
			p.s.Sends(a, t, cnt)
		},
	)
	return nil
}

////////
// 辅助
///////////////////////////////////////////////////////////////////////////////

//
// 批量发送数据包。
// 开启的协程数量由sendThreads配置决定。
//
func pingSends(ch <-chan net.Addr, send func(net.Addr)) {
	sem := make(chan struct{}, sendThreads)

	// 外部ch可正常关闭
	for dst := range ch {
		sem <- struct{}{}
		go func(ip net.Addr) {
			send(ip)
			<-sem
		}(dst)
	}
}

//
// 发送ICMP数据包。
// 对于syscall.ENOBUFS类错误重复尝试。
//
func icmpSend(conn *icmp.PacketConn, addr net.Addr, msg []byte, cancel func() bool) {
	for {
		if _, err := conn.WriteTo(msg, addr); err != nil {
			if neterr, ok := err.(*net.OpError); ok {
				if neterr.Err == syscall.ENOBUFS {
					if cancel() {
						break
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
			}
		}
		break
	}
}

//
// ICMP回显请求类型值。
//
func icmpType(dst net.Addr) (icmp.Type, error) {
	addr, ok := dst.(*net.IPAddr)
	if !ok {
		return nil, errors.New("argument not a *net.IPAddr address")
	}
	if isIPv4(addr.IP) {
		return ipv4.ICMPTypeEcho, nil
	}
	if isIPv6(addr.IP) {
		return ipv6.ICMPTypeEchoRequest, nil
	}
	return nil, fmt.Errorf("invalid IP address: %s", addr.IP)
}

//
// 构造ICMP消息数据包。
// 将当前时间设置到消息体（Body.Data）前段。
// 每次的 Seq 构造为一个随机值。
//
func icmpMsg(typ icmp.Type, code, id, seq int, dt []byte) ([]byte, error) {
	t := timeToBytes(time.Now())

	if len(dt) > 0 {
		t = append(t, dt...)
	}
	msg := icmp.Message{
		Type: typ,
		Code: code,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: t,
		},
	}
	return msg.Marshal(nil)
}

//
// 接收ICMP消息处理。
// 如果出错，data.Err包含了错误信息。
//
func icmpReceive(conn *icmp.PacketConn) *packet {
	var data packet
	var buf = make([]byte, recvBufSize)

	for {
		conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		_, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if neterr, ok := err.(*net.OpError); ok && neterr.Timeout() {
				continue
			}
		}
		data.Addr, data.Bytes, data.Err = addr, buf, err
		break
	}
	return &data
}

//
// 根据地址返回匹配的ICMP网络协议。
// 默认为IPv4类网络协议（ip4:icmp）
//
func addressProto(network, address string) (string, error) {
	ip := net.ParseIP(address)

	if isIPv4(ip) {
		return ipv4Proto[network], nil
	}
	if isIPv6(ip) {
		return ipv6Proto[network], nil
	}
	return "", fmt.Errorf("invalid IPv4/IPv6 address: %s", address)
}

//
// 解析回应消息。返回一个Echo对象。
//
func replyEchoParse(p *packet, network string) (*icmp.Echo, error) {
	addr, ok := p.Addr.(*net.IPAddr)
	if !ok {
		return nil, errors.New("bad IPAddr with reply message")
	}
	var proto int
	var buf []byte

	if isIPv4(addr.IP) {
		if network == "ip" {
			buf = ipv4Payload(p.Bytes)
		}
		proto = ProtocolICMP
	} else if isIPv6(addr.IP) {
		proto = ProtocolIPv6ICMP
	}
	m, err := icmp.ParseMessage(proto, buf)
	if err != nil {
		return nil, err
	}
	echo, ok := m.Body.(*icmp.Echo)
	if !ok {
		return nil, errors.New("message body not Echo type")
	}
	return echo, nil
}

//////////
// 小工具
///////////////////////////////////////////////////////////////////////////////

func isIPv4(ip net.IP) bool {
	return len(ip.To4()) == net.IPv4len
}

func isIPv6(ip net.IP) bool {
	return len(ip) == net.IPv6len
}

func ipv4Payload(b []byte) []byte {
	if len(b) < ipv4.HeaderLen {
		return b
	}
	hdrlen := int(b[0]&0x0f) << 2
	return b[hdrlen:]
}

func timeToBytes(t time.Time) []byte {
	nsec := t.UnixNano()
	b := make([]byte, 8)
	for i := uint8(0); i < 8; i++ {
		b[i] = byte((nsec >> ((7 - i) * 8)) & 0xff)
	}
	return b
}

func bytesToTime(b []byte) time.Time {
	var nsec int64
	for i := uint8(0); i < 8; i++ {
		nsec += int64(b[i]) << ((7 - i) * 8)
	}
	return time.Unix(nsec/1e9, nsec%1e9)
}
