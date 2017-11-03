// Package ping ICMP数据包回显模块。
// 支持多地址并发测试，地址提供者实现Address接口。
//
// 例:
//	ip, err := net.ResolveIPAddr("ip", os.Args[1])
//	if err != nil {
//		fmt.Println(err)
//		os.Exit(1)
//	}
//	// 本地监听地址 192.168.1.220
// 	p, _ := ping.NewPinger("ip", "192.168.1.220")
//
// 	// 启动接收监听服务
// 	p.Serve()
//
// 	// 实现Handler接口的实例并注册
// 	proc := Reverb{}
// 	p.Handle(proc)
//
//	p.Ping(ip, 3) // 最多3次失败再尝试
//
//	time.Sleep(time.Second * 5)
//	p.Stop() // 停止接收监听服务
//
// 参考：github.com/tatsushid/go-fastping
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
	sendGoroutines = 40

	// 接收消息缓存区大小
	recvBufferSize = 512
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
// Listen 启动监听。
// network 指采用的网络，"ip" 或 "udp"；
// address 为监听地址，可为IPv4或IPv6。空串视为IPv4地址："0.0.0.0"。
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

// Sender ICMP数据发送器。
type Sender struct {
	ID     int
	Seq    int
	conn   *icmp.PacketConn
	cancel func() bool

	// 附加数据（数据段起始8字节为时间戳）
	data []byte
	mu   sync.Mutex
}

//
// NewSender 创建一个发送器实例。
// @extra Ping包数据段附加字节序列
//
func NewSender(conn *icmp.PacketConn, cancel func() bool) *Sender {
	rand.Seed(time.Now().UnixNano())

	return &Sender{
		ID:     rand.Intn(0xffff),
		Seq:    rand.Intn(0xffff),
		conn:   conn,
		cancel: cancel,
	}
}

//
// Send 发送ICMP信息包。
// 并发安全。
//
func (s *Sender) Send(dst net.Addr) error {
	typ, err := icmpType(dst)
	if err != nil {
		return err
	}
	msg, err := icmpMsg(typ, 0, s.ID, s.Seq, s.Data(nil))
	if err != nil {
		return err
	}
	icmpSend(s.conn, dst, msg, s.cancel)
	return nil
}

//
// Data 设置/提取附加数据。
//
func (s *Sender) Data(dt []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dt == nil {
		return s.data
	}
	s.data = append(s.data, dt...)
	return nil
}

// Packet 回应信息包。
// 用于读取ICMP回应信息对外传递。
type Packet struct {
	addr  net.Addr
	bytes []byte
	err   error
}

// Receiver ICMP接收处理器。
type Receiver struct {
	conn *icmp.PacketConn
	stop chan struct{}
}

//
// NewReceiver 创建一个接收器实例。
//
func NewReceiver(conn *icmp.PacketConn, stop chan struct{}) *Receiver {
	return &Receiver{
		conn: conn,
		stop: stop,
	}
}

//
// Serve 启动接收服务。
// 每个接收器只应开启一个服务。
// 考虑效率，外部传入的recv可以适当缓存。
//
func (r *Receiver) Serve(recv chan<- *Packet) {
	go r.serve(recv)
}

func (r *Receiver) serve(recv chan<- *Packet) {
End:
	for {
		select {
		case recv <- icmpReceive(r.conn):
		case <-r.stop:
			break End
		}
	}
	close(recv) // 对外通知
}

////////////
// 应用元件
///////////////////////////////////////////////////////////////////////////////

// Pinger ICMP数据包发送器。
type Pinger struct {
	s       *Sender
	r       *Receiver
	h       Handler       // 回应处理器
	network string        // 连接网络（ip|udp）
	recv    chan *Packet  // 接收管道
	stop    chan struct{} // 停止信号量
	mu      sync.Mutex
}

//
// NewPinger 创建一个Ping实例。
// network 指网络名称，如："ip" 或 "udp"，
// address 指本地监听地址和端口，也可为空串。
//
func NewPinger(network, address string) (*Pinger, error) {
	conn, err := Listen(network, address)
	if err != nil {
		return nil, err
	}
	stop := make(chan struct{})

	return &Pinger{
		s:       NewSender(conn, goes.Canceller(stop)),
		r:       NewReceiver(conn, stop),
		stop:    stop,
		network: network,
		recv:    make(chan *Packet, 1),
	}, nil
}

//
// Serve 启动接收服务。
//
func (p *Pinger) Serve() {
	p.r.Serve(p.recv)
}

//
// Handle 设置回应处理器。
//
func (p *Pinger) Handle(h Handler) *Pinger {
	p.mu.Lock()
	p.h = h
	p.mu.Unlock()
	return p
}

//
// ExtraData 设置消息包附加数据。
// 该函数一般在实际发送数据包之前调用，但也并发安全。
//
func (p *Pinger) ExtraData(dt []byte) {
	if dt != nil {
		p.s.Data(dt)
	}
}

//
// Stop 终止发送。
// 该事件会导致当前Pinger实例相关的全部Go程终止。
// 注册的接收/失败操作也不会执行。
//
func (p *Pinger) Stop() {
	p.mu.Lock()
	close(p.stop)
	p.mu.Unlock()
}

//
// Ping 单次ping。
// 如果失败可再次尝试，取决于cnt参数。
// 不适用于并发（cnt参数将失去意义）。并发可使用包级Pings函数。
//
// @cnt 失败再尝试的次数。
//
func (p *Pinger) Ping(dst net.Addr, cnt int) error {
	p.s.Send(dst)

	var rd *Packet
	var ok bool
	for {
		rd, ok = <-p.recv
		if !ok || rd.err == nil || cnt <= 0 {
			break
		}
		cnt--
		p.s.Send(dst) // 失败再尝试
	}
	if !ok {
		return errors.New("abort by stop event")
	}
	if p.h == nil {
		// 未注册处理器
		return rd.err
	}
	if rd.err != nil {
		p.h.Fail(rd.addr, rd.err, p.Stop)
		return rd.err
	}
	body, err := replyEchoParse(rd, p.network)
	if err != nil {
		p.h.Fail(rd.addr, err, p.Stop)
		return err
	}
	p.h.Receive(rd.addr, p.s.ID, p.s.Seq, body, p.Stop)

	return nil
}

//
// PingLoop 向单个地址循环ping。
// 需要注册消息处理器，否则直接退出。
//
// @t 循环间隔时间
// @cnt 循环次数，-1表示无限
//
func (p *Pinger) PingLoop(dst net.Addr, t time.Duration, cnt int) error {
	var rd *Packet
	var ok bool
	for {
		go p.s.Send(dst)
		<-time.After(t)

		rd, ok = <-p.recv
		if !ok || cnt == 0 || p.h == nil {
			break
		}
		if rd.err != nil {
			p.h.Fail(rd.addr, rd.err, p.Stop)
			continue
		}
		body, err := replyEchoParse(rd, p.network)
		if err != nil {
			p.h.Fail(rd.addr, err, p.Stop)
			continue
		}
		p.h.Receive(rd.addr, p.s.ID, p.s.Seq, body, p.Stop)
		if cnt > 0 {
			cnt--
		}
	}
	if !ok {
		return errors.New("abort by stop event")
	}
	return nil
}

////////////
// 包级接口
///////////////////////////////////////////////////////////////////////////////

//
// Pings 对多个地址的单次ping。
// 失败的ping地址不会再尝试，但可以通过Fail()获得失败地址。
//
// 内部实现并发请求，外部不应用Pings本身对同一个p实例做并发。
// （对不同的p实例并发Pings没有问题）。
//
func Pings(p *Pinger, as Address) error {
	go pingSends(
		as.IPAddrs(goes.Canceller(p.stop)),
		p.s.Send)

	var rd *Packet
	var ok bool
	for {
		rd, ok = <-p.recv
		if !ok || p.h == nil {
			break
		}
		if rd.err != nil {
			go p.h.Fail(rd.addr, rd.err, nil)
			continue
		}
		body, err := replyEchoParse(rd, p.network)
		if err != nil {
			go p.h.Fail(rd.addr, err, p.Stop)
			continue
		}
		go p.h.Receive(rd.addr, p.s.ID, p.s.Seq, body, p.Stop)
	}
	if !ok {
		return errors.New("abort by stop event")
	}
	return nil
}

//
// 批量发送数据包。
// 开启的Go程数量由sendGoroutines配置决定。
//
func pingSends(ch <-chan net.Addr, send func(net.Addr) error) {
	pas := make(chan struct{}, sendGoroutines)

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
// Address IP地址生成器。
//
type Address interface {
	IPAddrs(cancel func() bool) <-chan net.Addr
}

// Handler 响应处理器。
// 包含成功时的接收和失败后的处理接口。
type Handler interface {
	// 正常接收操作。
	//  @id, @seq 为发送时的标记值，
	//  @dt 内有id/seq相应的字段对应，两者应相等。
	//  @stop 终止ping行为的反向控制函数（主要用于并发）。
	// stop可能类似：func() { close(ch) }
	Receive(a net.Addr, id, seq int, dt *icmp.Echo, stop func()) error

	// 错误时操作。
	// 包含连接读取错误和回应非EchoReply类型视为错误。
	// 并发时无法确定目标IP，@a为nil。
	// 注意：并发时可能出现大量调用。
	Fail(a net.Addr, err error, stop func())
}

////////////
// 私有工具
///////////////////////////////////////////////////////////////////////////////

//
// 发送ICMP数据包。
// 对于syscall.ENOBUFS类错误重复尝试。
//
func icmpSend(conn *icmp.PacketConn, addr net.Addr, msg []byte, cancel func() bool) {
	for {
		if _, err := conn.WriteTo(msg, ip); err != nil {
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
		return nil, errors.New("dst argument has not *net.IPAddr address")
	}
	if isIPv4(addr.IP) {
		return ipv4.ICMPTypeEcho, nil
	}
	if isIPv6(addr.IP) {
		return ipv6.ICMPTypeEchoRequest, nil
	}
}

//
// 构造ICMP消息数据包。
// 将当前时间设置到消息体（Body.Data）前段。
//
func icmpMsg(typ icmp.Type, code, id, seq int, dt []byte) ([]byte, error) {
	t := timeToBytes(time.Now())

	if len(dt) > 0 {
		t = append(t, dt...)
	}
	return &icmp.Message{
		Type: typ,
		Code: code,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: t,
		},
	}.Marshal(nil)
}

//
// 接收ICMP消息处理。
// 如果出错，data.err包含了错误信息。
//
func icmpReceive(conn *icmp.PacketConn) *Packet {
	var data Packet
	var buf = make([]byte, recvBufferSize)

	for {
		conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		_, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if neterr, ok := err.(*net.OpError); ok && neterr.Timeout() {
				continue
			}
		}
		data.addr, data.bytes, data.err = addr, buf, err
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
	return "", errors.New(address + " is not valid IPv4/IPv6 address")
}

//
// 解析回应消息。
//
func replyEchoParse(p *Packet, network string) (*icmp.Echo, error) {
	addr, ok := p.addr.(*net.IPAddr)
	if !ok {
		return nil, errors.New("bad IPAddr with reply message")
	}
	var proto int

	if isIPv4(addr.IP) {
		if network == "ip" {
			buf = ipv4Payload(p.bytes)
		}
		proto = ProtocolICMP
	} else if isIPv6(addr.IP) {
		proto = ProtocolIPv6ICMP
	}
	m, err := icmp.ParseMessage(proto, buf)
	if err != nil {
		return nil, err
	}
	if m.Type != ipv4.ICMPTypeEchoReply && m.Type != ipv6.ICMPTypeEchoReply {
		// 包含简单信息。
		return nil, fmt.Errorf("[%d/%d] not Echo Reply TYPE/CODE", m.Type, m.Code)
	}
	return m.Body, nil
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
	return time.Unix(nsec/1000000000, nsec%1000000000)
}

//
//
//
//
// Reverb 回响器（示例）
//////////////////////////
type Reverb struct{}

//
// Receive 成功接收。
func (r Reverb) Receive(a net.Addr, id, seq int, dt *icmp.Echo) error {
	if id != dt.ID || seq != dt.Seq {
		return errors.New("the id or seq not match message's item")
	}
	rtt := time.Since(bytesToTime(dt.Data[:TimeSliceLength]))
	fmt.Println(a, rtt)
	return nil
}

// Fail 失败显示。
func (r Reverb) Fail(a net.Addr, err error) {
	fmt.Println(a, err)
}
