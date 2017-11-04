// Package ping ICMP数据包回显模块。
// 支持多地址并发测试，地址提供者实现Address接口。
//
// ip网络需要特权运行。udp仅支持Linux或Darwin系统。
// $sudo go test
//
package ping

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
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
	//  @id, @seq 为发送时的标记值，
	//  @dt 内有id/seq相应的字段对应，两者应相等。
	//  @exit 终止ping行为的控制函数。
	Receive(a net.Addr, id int, echo *icmp.Echo, exit func()) error

	// 错误时的回调。
	// 包含连接读取错误和回应非EchoReply类型视为错误。
	// 并发时无法确定目标IP，a为nil。
	// 注意：并发时可能出现大量调用。
	Fail(a net.Addr, err error, exit func())
}

//
// Listen 创建监听连接。
// 配合Pinger使用。network实参应与NewPinger的传递值相同。
//  @network 指采用的网络，"ip" 或 "udp"
//  @address 为监听地址，可为IPv4或IPv6。空串视为IPv4地址："0.0.0.0"
//
func Listen(network, address string) (*icmp.PacketConn, error) {
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
	return icmp.ListenPacket(proto, address)
}

////////////
// 基本元件
///////////////////////////////////////////////////////////////////////////////

//
// ICMP数据发送器。
// （并发安全）
//
type sender struct {
	ID     int // Echo ID
	conn   *icmp.PacketConn
	cancel func() bool

	// 附加数据（原起始8字节为时间戳）
	extra []byte
	mu    sync.Mutex
}

//
// 创建一个发送器实例。
// @extra Ping包数据段附加字节序列
//
func newSender(conn *icmp.PacketConn, cancel func() bool) *sender {
	rand.Seed(time.Now().UnixNano())

	return &sender{
		ID:     rand.Intn(0xffff),
		conn:   conn,
		cancel: cancel,
	}
}

//
// Send 对目标地址发送ICMP信息包。
// 单次发送。并发安全。
//
func (s *sender) Send(dst net.Addr) error {
	typ, err := icmpType(dst)
	if err != nil {
		return err
	}
	msg, err := icmpMsg(typ, 0, s.ID, s.getExtra())
	if err != nil {
		return err
	}
	icmpSend(s.conn, dst, msg, s.cancel)
	return nil
}

//
// Extra 设置附加数据。
// 并发安全，可在任意时刻设置该数据。
//
func (s *sender) Extra(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	s.extra = append(s.extra, data...)
	s.mu.Unlock()
}

//
// 并发安全的取附加数据。
//
func (s *sender) getExtra() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extra
}

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
// receiver ICMP接收处理器。
// （并发安全）
//
type receiver struct {
	conn *icmp.PacketConn
	stop chan struct{}
	recv chan *packet
}

//
// 创建一个接收器实例。
//
func newReceiver(conn *icmp.PacketConn, stop chan struct{}) *receiver {
	return &receiver{
		conn: conn,
		stop: stop,
		recv: make(chan *packet, 1),
	}
}

//
// Serve 启动接收服务。
// 每个接收器只应开启一个服务（调用一次）。
//
func (r *receiver) Serve() {
	go func() {
	End:
		for {
			select {
			case r.recv <- icmpReceive(r.conn):
			case <-r.stop:
				break End
			}
		}
		close(r.recv) // 对外通知
	}()
}

//
// Recv 返回接收的信息包。
// ok为false表示终止接收（接收服务已停止）。
//
func (r *receiver) Recv() (v *packet, ok bool) {
	v, ok = <-r.recv
	return
}

////////////
// 应用元件
///////////////////////////////////////////////////////////////////////////////

//
// Pinger ICMP ping 处理器。
// （并发安全）
//
type Pinger struct {
	s       *sender       // 消息发送器
	r       *receiver     // 接收处理器
	h       Handler       // 回应处理句柄
	network string        // 网络名（ip|udp）
	stop    chan struct{} // 停止信号量
}

//
// NewPinger 创建一个Ping实例。
// network 为网络名称，如："ip" 或 "udp"，需与Linten调用传递的值一致。
//
func NewPinger(network string, conn *icmp.PacketConn, calls Handler) (*Pinger, error) {
	stop := make(chan struct{})

	return &Pinger{
		s:       newSender(conn, goes.Canceller(stop)),
		r:       newReceiver(conn, stop),
		h:       calls,
		network: network,
		stop:    stop,
	}, nil
}

//
// Serve 启动接收服务。
//
func (p *Pinger) Serve() {
	p.r.Serve()
}

//
// ExtraData 设置消息包附加数据。
// 该设置需要在实际调用 Ping 系列之前执行。
//
func (p *Pinger) ExtraData(data []byte) {
	p.s.Extra(data)
}

//
// Exit 结束处理。
// 可能由外部处理操作触发，或用户直接调用。
//
// 结束后的实例不能再次使用。
//
func (p *Pinger) Exit() {
	goes.Close(p.stop)
}

//
// Ping 单次ping。
// 如果失败可再次尝试，cnt 为失败再尝试的次数。
//
func (p *Pinger) Ping(dst net.Addr, cnt int) error {
	p.s.Send(dst)

	var rd *packet
	var ok bool
	for {
		rd, ok = p.r.Recv()
		if !ok || rd.Err == nil || cnt <= 0 {
			break
		}
		cnt--
		p.s.Send(dst) // 失败再尝试
	}
	if p.h == nil {
		// 无处理器
		return rd.Err
	}
	return p.process(rd)
}

//
// PingLoop 向单个地址循环ping。
// 需要注册消息处理器，否则直接退出。
// 	@t 循环间隔时间
// 	@cnt 循环次数，-1表示无限
//
func (p *Pinger) PingLoop(dst net.Addr, t time.Duration, cnt int) error {
	var rd *packet
	var ok bool
	for {
		go p.s.Send(dst)
		<-time.After(t)

		rd, ok = p.r.Recv()
		if !ok || cnt == 0 || p.h == nil {
			break
		}
		p.process(rd)
		// 负值无限循环
		if cnt > 0 {
			cnt--
		}
	}
	return nil
}

//
// Pings 对序列地址的单次ping。
// 同样需要存在消息处理器，否则直接返回。
//
// 失败的ping地址不会再尝试，但可以通过Fail()获得失败地址。
//
func (p *Pinger) Pings(as Address) error {
	go pingSends(
		as.IPAddrs(goes.Canceller(p.stop)),
		p.s.Send)

	var rd *packet
	var ok bool
	for {
		rd, ok = p.r.Recv()
		if !ok || p.h == nil {
			break
		}
		go p.process(rd)
	}
	return nil
}

//
// 处理接收的数据包。
// Fail 或 Receive 回调，返回已存在或回调返回的错误。
//
func (p *Pinger) process(rd *packet) error {
	if rd == nil {
		return errors.New("package data is nil")
	}
	if rd.Err != nil {
		p.h.Fail(rd.Addr, rd.Err, p.Exit)
		return rd.Err
	}
	echo, err := replyEchoParse(rd, p.network)
	if err != nil {
		p.h.Fail(rd.Addr, err, p.Exit)
		return err
	}
	return p.h.Receive(rd.Addr, p.s.ID, echo, p.Exit)
}

////////////
// 私有辅助
///////////////////////////////////////////////////////////////////////////////

//
// 批量发送数据包。
// 开启的协程数量由sendThreads配置决定。
//
func pingSends(ch <-chan net.Addr, send func(net.Addr) error) {
	pas := make(chan struct{}, sendThreads)

	// 外部ch可正常关闭
	for dst := range ch {
		pas <- struct{}{}
		go func(ip net.Addr) {
			send(ip)
			<-pas
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
func icmpMsg(typ icmp.Type, code, id int, dt []byte) ([]byte, error) {
	t := timeToBytes(time.Now())

	if len(dt) > 0 {
		t = append(t, dt...)
	}
	msg := icmp.Message{
		Type: typ,
		Code: code,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  rand.Intn(0xffff),
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

//////////////
// 通用小工具
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
