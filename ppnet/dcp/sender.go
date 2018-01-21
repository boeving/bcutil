package dcp

/////////////////////
/// DCP 内部服务实现。
/// 流程
/// 		客户端：应用请求 >> [发送]； [接收] >> 写入应用。
/// 		服务端：[接收] >> 询问应用，获取io.Reader，读取 >> [发送]。
///
/// 发送方
/// ------
/// - 根据对方的请求，从注册的响应服务应用获取响应数据。
/// - 一个交互期的初始阶段按基准速率匀速发送。收到第一个确认后，获得网络基本容量。
/// - 后续的发送附带有效的发送距离（非零。减去进度线序列号和网络容量）。
/// - 由对方发来的数据报的确认距离评估丢包情况，决定重发。
///
///
/// 顺序并发
/// - 数据ID按应用请求的顺序编号，并顺序发送其首个分组。之后各数据体Go程自行发送。
/// - 数据体Go程以自身动态评估的即时速率发送，不等待确认，获得并行效果。
/// - 数据体Go程的速率评估会间接作用于全局基准速率（基准线）。
/// - 一个交互期内起始的数据体的数据ID随机产生，数据体内初始分组的序列号也随机。
///   注：
///   首个分组按顺序发送可以使得接收端有所凭借，而不是纯粹的无序。
///   这样，极小数据体（仅有单个分组）在接收端的丢包判断就容易了（否则很难）。
///
/// 有序重发
/// - 每个数据体权衡确认距离因子计算重发。
/// - 序列号顺序依然决定优先性，除非收到接收端的主动重发请求。
///
/// 速率评估
/// - 速率以发送间隔的时间来表达，时间越长速率则越慢。
/// - 各数据体自行评估自己的发送间隔时间并休眠，向总速率评估器传递发送距离变化量和丢包判断。
/// - 总速率评估器根据各数据体的发送距离变化量和丢包情况，评估当前实际发送速率。
/// - 总速率评估器的基准速率被定时评估调优，同时它也作为各数据体的基准速率。
/// - 每个交互期的数据量也用于基准速率调优参照。
/// 注：
/// 各数据体按自身评估的速率并发提供构造好的数据报，由总发送器统一发送。
///
///////////////////////////////////////////////////////////////////////////////

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/goes"
)

// 基础常量设置。
const (
	BaseRate    = 10 * time.Millisecond  // 基础速率。初始默认发包间隔
	RateLimit   = 100 * time.Microsecond // 极限速率。万次/秒
	SendTimeout = 500 * time.Millisecond // 发送超时
	TimeoutMax  = 120 * time.Second      // 发送超时上限
	SeqLimit    = 0xffff                 // 序列号上限（不含）
)

// 基本参数常量
const (
	capRange           = 5 * BaseRate           // 变幅容度
	lossFall           = 2 * BaseRate           // 丢包速率跌幅
	lossStep           = 4                      // 丢包一次的步进计数
	lossLimit          = 5.0                    // 丢包界限（待统计调优）
	lossRecover        = 15                     // 丢包恢复总滴答数
	lossTick           = 1 * time.Second        // 丢包恢复间隔（一个滴答）
	lossBase           = 20                     // 丢包总权衡基值（线性减速）
	baseTick           = 10 * time.Second       // 基准速率评估间隔（一个滴答）
	baseRatio          = 0.1                    // 基准速率调整步进比率
	dataFull     int64 = 1024 * 576             // 满载数据量（概略值）
	sendBadTries       = 3                      // 发送出错再尝试次数
	sendBadSleep       = 100 * time.Millisecond // 发送出错再尝试暂停时间
	sendPackets        = 40                     // 发送通道缓存（有序性）
)

// END关联全局变量。
// 可能根据网络状态而调整为适当的值，但通常无需修改。
// （非并发安全）
var (
	SendEndtime = 10 * time.Second // 发送END包后超时结束时限
)

//
// 总发送器（发送总管）。
// 按自身评估的速率执行实际的网络发送。
//
// 通过缓存信道获取各数据体Go程的数据包。
// 根据接收服务器的要求发送ACK确认，尽量携带数据发送。
// 各Go程的递送无顺序关系，因此实现并行的效果。
//
// 外部保证每个数据体的首个分组优先递送，
// 因此高数据ID的数据体必然后发送，从而方便接收端判断是否丢包。
//
// 一个4元组两端连系对应一个本类实例。
//
// 注记：
// 用单向链表存储确认信息申请，以保持先到先用的顺序。
//
type xSender struct {
	Conn    *connWriter          // 数据报网络发送器
	Eval    *rateEval            // 速率评估器
	Post    <-chan *packet       // 待发送数据包通道（有缓存，一定有序性）
	Acks    <-chan *ackReq       // 待确认信息包通道（同上）
	RcvPool map[uint16]*recvServ // 接收服务器池（key:数据ID#RCV）
}

//
// 启动发送服务。
//
func (x *xSender) Serve(stop *goes.Stop) {
	for {
		var p *packet
		var req *ackReq

		select {
		case <-stop.C:
			return

		case p = <-x.Post:
			// 忽略响应出错
			// 接收端可等待超时后可重新请求。
			if p == nil {
				continue
			}
			req = x.AckReq()

		case req = <-x.Acks:
			if req == nil {
				continue
			}
			p = x.Packet()
		}
		// 失败后退出
		if n, err := x.Send(p, req); err != nil {
			log.Printf("write to net %d tries, but all failed.", n)
			return
		}
		time.Sleep(x.Eval.Rate())
	}
}

//
// 发送请求。
// 由外部客户请求调用，有更高的优先级。
//
func (x *xSender) Request(res []byte) error {
	//
}

//
// 从通道提取数据报。
// 非阻塞，如果通道无数据返回一个空包。
//
func (x *xSender) Packet() *packet {
	select {
	case p := <-x.Post:
		return p
	default:
		return x.emptyPacket()
	}
}

//
// 从通道提取确认申请信息。
// 非阻塞，如果通道无数据返回nil。
//
func (x *xSender) AckReq() *ackReq {
	select {
	case r := <-x.Acks:
		return r
	default:
		return nil
	}
}

//
// 创建一个空数据报。
// 通常用于发送一个单独的确认（无携带数据）。
//
func (x *xSender) emptyPacket() *packet {
	h := header{
		SID: 0xffff,
		Seq: 0xffff,
	}
	return &packet{&h, nil}
}

//
// 发送数据报。
// 按自身的速率（休眠间隔）发送数据报，
// 如果失败会适当多次尝试，依然返回错误时，外部通常应结束服务。
// 返回的整数值为失败尝试的次数。
//
func (x *xSender) Send(p *packet, req *ackReq) (int, error) {
	if req != nil {
		x.SetAcks(p, req)
	}
	var err error
	var cnt int

	for ; cnt < sendBadTries; cnt++ {
		if _, err = x.Conn.Send(p); err == nil {
			break
		}
		log.Println(err)
		time.Sleep(sendBadSleep)
	}
	return cnt, err
}

//
// 设置确认信息到数据报头。
// 包含接收数据ID、接收号、确认距离或重发申请。
//
func (x *xSender) SetAcks(p *packet, req *ackReq) {
	if req.Rtp {
		p.Set(RTP)
	}
	if req.Dist > 0 {
		p.AckDst = uint(req.Dist)
	}
	if req.Bye {
		p.Set(BYE)
	}
	p.RID = req.ID
	p.Rcv = req.Rcv
}

//
// 首个分组发送服务。
// 保持各数据体首个分组发送的有序性。持续服务。
//
type firstSends struct {
}

//
// 末尾包确认发送服务。
// 各数据体末尾END包确认的持续服务，保证发送方的结束。
//
// 如果收到BYE包则停止，并可移除其END条目。
// 如果未收到BYE包，则按评估的速率重发END确认，最多endAcks次。
//
type endSends struct {
}

//
// 数据报发送器。
// 一个响应服务对应一个本类实例。
// - 从响应器接收数据负载，构造数据报递送到总发送器。
// - 根据对端确认距离评估丢包重发，重构丢失包之后已发送的数据报。
// - 管理递增的序列号。
//
// 退出发送：
// - 接收到END确认后（ackRecv），发送一个BYE通知即退出。
// - 接收端最多重复endAcks次END确认，如果都丢失，则由超时机制退出。
//
// 接收项（对端设置的）：
// - Rcv 	接收号
// - Dist 	确认距离
// - RTP 	请求重发标记
// - RST 	重置发送标记
//
// 注记：
// 丢失的包不可能跨越一个序列号回绕周期，巨大的发送距离也会让发送停止。
//
type servSend struct {
	ID      uint16           // 数据ID#SND
	Post    chan<- *packet   // 数据报递送通道（有缓存）
	Bye     chan<- *ackReq   // 结束通知（BYE）
	Loss    <-chan int       // 丢包重发通知
	Pmtu    <-chan int       // PMTU大小获取
	Reset   <-chan int       // 重置发送通知
	Eval    *evalRate        // 速率评估器
	Resp    *response        // 响应器
	Recv    *ackRecv         // 确认接收器
	Timeout <-chan time.Time // END确认超时结束
}

//
// 返回数据片合法大小。
//
func (s *servSend) DataSize() int {
	return <-s.Pmtu - headAll
}

//
// 启动发送服务（阻塞）。
// 传入的stop用于外部异常终止服务。
// 丢失的包可能简单重传或重组（小包），不受速率限制立即发送。
// 注记：
// 丢包与正常的发送在一个Go程里处理，无并发问题。
//
func (s *servSend) Serve(re *rateEval, stop *goes.Stop) {
	var rst bool
	buf := make(map[int]*packet)

	rand.Seed(time.Now().UnixNano())
	// 随机初始值
	seq := rand.Intn(0xffff-1) % SeqLimit

	// 正常发送通道，缓存>0
	snd := make(chan *packet, 1)

	for {
		size := s.DataSize()
		select {
		case <-stop.C:
			return // 外部强制结束
		case <-s.Timeout:
			// END包发送后超时，
			// 接收端会重复多次END包的确认，因此通常不会至此。
			stop.Exit()
			return

		case i := <-s.Reset:
			s.Reslice(s.Buffer(buf, i, seq), size)
			seq = i
			rst = true
			log.Printf("reset sending from %d", i)

		case i, ok := <-s.Loss:
			if !ok {
				go s.End()
				return // END 确认，正常结束
			}
			p := buf[i]
			if rst || p == nil {
				break // 已被重构，忽略
			}
			switch {
			case len(p.Data) == size:
				// 简单重传
				s.Post <- p
				log.Printf("lost packet [%d] retransmission.", i)

			case len(p.Data) < size:
				// 小包重组
				p, n := s.Combine(buf, i, seq, size)
				s.Post <- p
				log.Printf("repacketization from %d to %d", i, i+n-1)

			default:
				// 全部重构
				s.Reslice(s.Buffer(buf, i, seq), size)
				rst = true
				seq = i
				log.Printf("rebuild all packets from %d", i)
			}

		// 结束后阻塞（等待确认）。
		// 不影响丢包后的重构发送或对端请求重置发送。
		case p := <-s.Send(seq, size, rst, snd):
			// 速率控制
			time.Sleep(s.Eval.Rate(int(p.SndDst), re.Base()))
			s.Post <- p

			buf[seq] = p
			seq = (seq + 1) % SeqLimit
			rst = false
		}
	}
}

//
// 发送结束通知（BYE）
// 用于发送总管设置一个BYE通知（与携带数据无关）。
//
func (s *servSend) End() {
	s.Bye <- &ackReq{
		ID:  s.ID,
		Rcv: 0xffff,
		Bye: true,
	}
}

//
// 返回一个正常发送数据报的读取信道。
// 传递并返回通道，主要用于读取结束后的阻塞控制。
//
func (s *servSend) Send(seq, size int, rst bool, pch chan *packet) <-chan *packet {
	p := s.Build(seq, size, rst)
	if p == nil {
		s.Timeout = time.After(SendEndtime)
		// 阻塞
		return nil
	}
	s.Timeout = nil
	pch <- p // buffer > 0
	return pch
}

//
// 提取map集内特定范围的分片集。
// end可能小于beg，如果end在seqLimit的范围内回绕的话。
// 注：会同时删除目标值。
//
func (s *servSend) Buffer(mp map[int]*packet, beg, end int) [][]byte {
	cnt := roundSpacing(beg, end, SeqLimit)
	buf := make([][]byte, 0, cnt)

	for i := 0; i < cnt; i++ {
		p := mp[beg]
		if p == nil {
			continue
		}
		buf = append(buf, p.Data)
		delete(mp, beg)
		beg = (beg + 1) % SeqLimit
	}
	return buf
}

//
// 对分片集重新分片。
// 这在丢包且路径MTU变小的情况下发生。
// 新的分片集会返还到响应器。
//
func (s *servSend) Reslice(bs [][]byte, size int) {
	if len(bs) == 0 {
		return
	}
	// 可能为END包重构，
	// 因此需预防重置为无效值。
	s.Recv.SetEnd(0xffff)

	s.Resp.Shift(pieces(bytes.Join(bs, nil), size))
}

//
// 创建一个报头。
//
func (s *servSend) Header(ack, seq int) *header {
	return &header{
		SID:    s.ID,
		Seq:    uint16(seq),
		SndDst: uint(roundSpacing(ack, seq, SeqLimit)),
	}
}

//
// 合并构造（重组分片）。
// 会尽量合并后续连续小包（最多15个），但也可能无法合并。
// 如果成功合并，头部会设置RST标记。
//
// hist 为历史栈，被合并的条目会被清除（后续丢包可能定位到）。
// end 为合法的最后位置（当前序列号）。
// 返回重组的包或首个包（无法合并时）和重组的包个数。
//
// 注：
// - 重组的首个包外部保证不为nil。
// - 除首个包外，删除被重组的包存储（后续丢失处理排除）。
//
func (s *servSend) Combine(hist map[int]*packet, beg, max, size int) (*packet, int) {
	seq := (beg + 1) % SeqLimit
	cnt := roundSpacing(beg, max, SeqLimit)
	if cnt > 15 {
		cnt = 15
	}
	var sum int
	var end bool
	buf := make([][]byte, 1, cnt+1)
	// 首个备用
	first := hist[beg]
	buf[0] = first.Data

	for i := 0; i < cnt; i++ {
		p, ok := hist[seq]
		if !ok {
			break
		}
		sum += len(p.Data)
		if sum > size {
			break
		}
		buf = append(buf, p.Data)
		end = p.END()
		delete(hist, seq)
		seq = (seq + 1) % SeqLimit
	}
	n := len(buf)
	if n == 1 {
		return first, 1
	}
	return s.build(bytes.Join(buf, nil), beg, n-1, true, end), n
}

//
// 构建数据报。
// 从响应器获取数据片构造数据报。
// 返回nil表示读取出错（非io.EOF）。
// 返回的布尔值表示是否读取结束。
//
func (s *servSend) Build(seq, size int, rst bool) *packet {
	b, err := s.Resp.Get(size)
	if b == nil {
		return nil
	}
	if err != nil && err != io.EOF {
		log.Printf("read [%d] error: %s.", s.ID, err)
		return nil
	}
	return s.build(b, seq, 0, rst, err == io.EOF)
}

//
// 构建数据报。
//
func (s *servSend) build(b []byte, seq, rpz int, rst, end bool) *packet {
	h := s.Header(s.Recv.Acked(), seq)
	if end {
		h.Set(END)
		s.Recv.SetEnd(seq)
	}
	if rst {
		h.Set(RST)
	}
	if rpz != 0 {
		h.SetRPZ(rpz)
	}
	return s.packet(h, b)
}

//
// 创建一个数据报。
//
func (s *servSend) packet(h *header, b []byte) *packet {
	return &packet{header: h, Data: b}
}

//
// 确认信息。
// 对端对本端响应的确认信息。
//
type rcvInfo struct {
	Rcv  int  // 接收号
	Dist int  // 确认距离
	Rtp  bool // 重传请求
}

//
// 确认接收器。
// 接收对端的确认号和确认距离，并评估是否丢包。
// - 丢包时通过通知信道传递丢失包的序列号。
// - 如果包含END标记的数据报已确认，则关闭通知信道。
//
type ackRecv struct {
	Rcvs  <-chan *rcvInfo // 接收信息传递信道
	Loss  *lossEval       // 丢包评估器
	end   int             // END包的序列号
	acked int             // 确认号（进度）存储
	mu    sync.Mutex      // acked同步锁
}

//
// 启动接收&评估服务。
// 传入的stop可用于异常终止服务。
//
// sch 为丢包序列号通知信道。
// 当收到END信息时关闭通知信道，发送器据此退出服务。
//
func (a *ackRecv) Serve(sch chan<- int, re *rateEval, stop *goes.Stop) {
	for {
		select {
		case <-stop.C:
			return
		case ai := <-a.Rcvs:
			if a.isEnd(ai.Rcv) {
				close(sch)
				return
			}
			if ai.Rtp {
				sch <- ai.Rcv
				continue
			}
			a.mu.Lock()
			if ai.Dist == 1 {
				a.acked = (a.acked + 1) % SeqLimit
			} else {
				a.acked = roundBegin(ai.Rcv, ai.Dist, SeqLimit)
			}
			if a.lost(ai.Dist, re) {
				sch <- (a.acked + 1) % SeqLimit
			}
			a.mu.Unlock()
		}
	}
}

//
// 返回当前进度（确认号）。
//
func (a *ackRecv) Acked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acked
}

//
// 设置END包序列号。
//
func (a *ackRecv) SetEnd(rcv int) {
	a.mu.Lock()
	a.end = rcv
	a.mu.Unlock()
}

//
// 是否接收到END包的确认。
//
func (a *ackRecv) isEnd(rcv int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return rcv == a.end
}

//
// 评估丢包。
// dist 为确认距离，re 为总速率评估器。
//
func (a *ackRecv) lost(dist int, re *rateEval) bool {
	return dist > 1 &&
		a.Loss.Eval(float64(dist), re.Base())
}

//
// 请求响应器。
// 提供对端请求需要的响应数据。
// 非并发安全，外部保证单Go程处理。
// 一个数据体对应一个本类实例。
//
type response struct {
	rb   *bufio.Reader // 响应读取器
	back [][]byte      // 环回的前置分片集
	done bool          // 读取结束（不影响back）
}

func newResponse(r io.Reader) *response {
	return &response{
		rb: bufio.NewReader(r),
	}
}

//
// 获取下一个分片。
// 优先从back的存储从提取。
// 作为io.EOF的error保证与最后一片数据一起返回。
//
func (r *response) Get(size int) ([]byte, error) {
	if len(r.back) == 0 {
		return r.read(size)
	}
	// 外部保证互斥调用
	return r.fromBack(size)
}

//
// 从原始的流读取数据。
// 如果读取到数据尾部，io.EOF保证与最后一片数据同时返回。
// 这便于上级标注该片数据为最后一片。
//
// 如果一开始就没有可读的数据，返回的数据片为nil。
//
func (r *response) read(size int) ([]byte, error) {
	if r.done {
		return nil, nil
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(r.rb, buf)

	if n == 0 {
		r.done = true
		return nil, err
	}
	if err == nil {
		// 末尾探测
		_, err = r.rb.Peek(1)
	}
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if err == io.EOF {
		r.done = true
	}
	return buf[:n], err
}

//
// 从环回集中提取首个成员。
// 外部确认环回集不为空。
// back中的大数据片会被重构为指定的大小。
//
func (r *response) fromBack(size int) ([]byte, error) {
	if len(r.back[0]) > size {
		// 重构
		r.back = pieces(bytes.Join(r.back, nil), size)
	}
	b := r.back[0]
	r.back = r.back[1:]

	var err error
	if r.done && len(r.back) == 0 {
		err = io.EOF
	}
	return b, err
}

//
// 环回前置插入。
// 用于MTU突然变小后，已读取的数据返还重新分组。
// 非并发安全，外部保证互斥调用。
//
func (r *response) Shift(bs [][]byte) {
	r.back = append(bs, r.back...)
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
// 速率评估相关
///////////////////////////////////////////////////////////////////////////////

//
// 总速率评估。
// 为全局总发送器提供发送速率依据。
// 发送数据量影响基准速率，各数据体发送距离的增减量影响当前速率。
//
// 距离：
// 各数据体的发送距离增减量累计，越小说明整体效率越好。
// 但距离增减量累计不能体现初始的发送距离。
//
// 丢包：
// 各数据体评估丢包后会发送一个通知，用于总发送器评估调整当前速率。
// 这通常是由于初始发送的速率就偏高。
//
// 数据量：
// - 数据量越小，基准速率向慢趋稳。降低丢包评估重要性。
// - 数据量越大，基准速率趋快愈急。丢包评估更准确。
//
// 自调优：
// 根据长时间的当前速率评估计算基准速率，调优自适应。
//
type rateEval struct {
	base     float64         // 基准速率
	rate     float64         // 当前速率
	mu       sync.Mutex      // base并发安全
	dataSize int64           // 数据量
	ease     easeRate        // 速率生成器
	dist     <-chan int      // 发送距离增减量（负为减）
	loss     <-chan struct{} // 丢包通知
}

//
// 新建一个评估器。
// 其生命期通常与一个对端连系相关联，连系断开才结束。
//
// total 是一个待调优的估计值，可能取1.5倍个体上限（255）。
// 该值越大，随着距离增大的速率曲线越平缓。
//
// dch 与所有数据体发送实例共享（一对连系），收集相关信息。
// 通常为一个有缓存大小的信道。
//
func newRateEval(total float64, dch <-chan int, lch <-chan struct{}) *rateEval {
	return &rateEval{
		base: float64(BaseRate),
		rate: float64(BaseRate),
		ease: newEaseRate(total),
		dist: dch,
		loss: lch,
	}
}

//
// 获取基准速率值。
// 主要供各数据体的发送进程参考。
//
func (r *rateEval) Base() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base
}

//
// 获取当前的发送速率。
//
func (r *rateEval) Rate() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Duration(r.rate)
}

//
// 启动评估服务。
// 包含距离增减量因子和丢包信息。
// 评估是一个持续的过程，由外部读取当前评估结果。
//
// 可通过返回的stop中途中断评估服务（然后以不同的zoom重启）。
// 注意最好只有一个服务在运行。
//
// 数据量对基准速率的影响由外部调用产生。
//
func (r *rateEval) Serve(zoom float64) *goes.Stop {
	stop := goes.NewStop()

	go r.lossEval(stop)
	go r.distEval(zoom, stop)
	go r.baseEval(stop)

	return stop
}

//
// 基准速率评估（阻塞）。
// 按一定的节奏用参考前速率调优基准速率。
//
func (r *rateEval) baseEval(stop *goes.Stop) {
	tick := time.NewTicker(baseTick)
	davg := 0.0
Loop:
	for {
		select {
		case <-stop.C:
			break Loop
		case <-tick.C:
			r.mu.Lock()
			davg += r.rate - r.base
			davg /= 2
			r.base = r.baseRate(davg, r.base)
			r.mu.Unlock()
		}
	}
	tick.Stop()
}

//
// 评估基准速率。
// - 如果当前速率与基准速率差不多或更高，适量提速。
// - 如果当前速率多数情况下低于基准速率，适量减速。
// 注：
// 最终基准速率不可高于全局设定的极限速率（RateLimit）。
//
// avg 为两个速率差的积累平均值。负值加速，正值减速。
//
func (r *rateEval) baseRate(avg, base float64) float64 {
	v := base + avg/base*baseRatio

	if v < float64(RateLimit) {
		return float64(RateLimit)
	}
	return v
}

//
// 评估丢包影响。
// 随着丢包次数越多，当前速率增量下降。
// 但随着时间移动，当前速率会慢慢恢复。
//
// 下降为线性增量，恢复为渐近式向基线靠拢。
//
func (r *rateEval) lossEval(stop *goes.Stop) {
	var x int // 加速参量
	var y int // 减速参量
	// 恢复滴答
	tick := time.NewTicker(lossTick)
Loop:
	for {
		select {
		case <-stop.C:
			break Loop // 退出
		case <-r.loss:
			if x > 0 {
				x-- // 恢复累积
			}
			// 无限递增
			y++
			r.mu.Lock()
			r.rate += r.base * (float64(y) / lossBase)
			r.mu.Unlock()
		case <-tick.C:
			if y > 0 {
				y-- // 减速消解
			}
			if x > lossRecover {
				break // 停止加速
			}
			x++
			r.mu.Lock()
			r.rate = r.recover(float64(x), lossRecover)
			r.mu.Unlock()
		}
	}
	tick.Stop()
}

//
// 恢复，逐渐向基线靠拢。
// x 值为当前滴答数。
// d 值为全局设置 lossRecover。
//
func (r *rateEval) recover(x, d float64) float64 {
	if x >= d {
		return r.base
	}
	return r.rate - x/d*(r.rate-r.base)
}

//
// 评估距离增减量影响（当前速率）。
// 参考各数据体发送来的发送距离增减量评估当前速率。
// 增减量累积为负时，会提升当前速率。
//
// 各数据体在自我休眠（发送间隔）前应当先传递距离数据。
//
func (r *rateEval) distEval(zoom float64, stop *goes.Stop) {
	var d float64
	for {
		select {
		case <-stop.C:
			return
		case v := <-r.dist:
			d += float64(v)
		}
		r.mu.Lock()
		r.rate += r.ease.Send(d, r.base, float64(capRange), zoom)
		r.mu.Unlock()
	}
}

//
// 数据量影响。
// 当达到最大值之后即保持不变，数据量只会增加不会减小。
// 数据量因子表达的是起始传输的持续性，故无减小逻辑。
//
// 通常，每发送一个数据报就会调用一次。size应当非负。
// 并发安全。
//
func (r *rateEval) Flow(size int64) {
	if r.dataSize >= dataFull {
		return
	}
	r.dataSize += size
	if r.dataSize > dataFull {
		r.dataSize = dataFull
	}
	r.mu.Lock()
	r.base += r.ratioFlow(r.dataSize) * float64(BaseRate)
	r.mu.Unlock()
}

//
// 数据量速率评估。
// 返回一个基础速率的增减比率（-0.4 ~ 0.6）。
// 共分100个等级，数据量越大，返回值越小。
// 注：
// [-0.4~0.6] 是一个直觉估值，待测试调优。
// 大概情形为：如果初始数据就较大，则趋快（先减），否则趋慢。
//
func (r *rateEval) ratioFlow(amount int64) float64 {
	if amount >= dataFull {
		return -0.4
	}
	return 0.6 - float64(amount*100/dataFull)/100
}

//
// 单元数据流。
// 对应单个的数据体，缓存即将发送的数据。
// 提供数据量大小用于速率评估。
//
type flower struct {
	buf  *bufio.Reader // 数据源
	size int64         // 剩余数据量
}

func newFlower(r io.Reader, size int64) *flower {
	return &flower{bufio.NewReader(r), size}
}

//
// 切分目标大小的分组。
// 返回的切片大小可能小于目标大小（数据已经读取完）。
//
func (f *flower) Piece(size int) ([]byte, error) {
	b := make([]byte, size)
	n, err := f.buf.Read(b)
	f.size -= int64(n)

	if err != nil {
		f.size = 0
	}
	return b[:n], err
}

//
// 返回数据量剩余大小。
// 容错初始大小设置错误的情况。
//
func (f *flower) Size() int64 {
	if f.size < 0 {
		return 0
	}
	return f.size
}

//
// 评估速率。
// 参考当前动态基准速率计算发送速率，用于外部发送休眠。
// 包含普通状态和丢包状态下的速率获取。
// 每一个数据体发送实例包含一个本类实例。
//
// 距离：
// - 发送距离越大，是因为对方确认慢，故减速。
// - 确认距离越大，丢包概率越大，故减速。
//
// 注：
// 对外传递发送距离增减量，用于总速率评估器（rateEval）参考。
// 不含初始段的匀速（无评估参考）。
//
type evalRate struct {
	*lossEval            // 丢包评估器
	Zoom      float64    // 发送曲线缩放因子
	Ease      easeRate   // 曲线速率生成
	Dist      chan<- int // 发送距离增减量通知
	prev      int        // 前一个发送距离暂存
}

//
// 计算发送速率。
// 对外传递当前发送距离增减量。评估即时速率。
// d 为发送距离（1-1023）。
// base 为当前动态基准速率。
//
// 每发送一个数据报调用一次。
//
func (e *evalRate) Rate(d int, base float64) time.Duration {
	if e.prev == 0 {
		e.prev = d
	}
	// 可能阻塞（很难）
	// 传递相对增减量更合适。
	e.Dist <- int(d - e.prev)
	e.prev = d

	if e.Lost() {
		return e.lossEval.Rate(float64(d), base)
	}
	return time.Duration(
		e.Ease.Send(float64(d), base, e.Zoom, float64(capRange)),
	)
}

//
// 丢包评估器（发送方）。
// 根据确认距离评估是否丢包，提供丢包阶段速率。
//
type lossEval struct {
	Zoom float64    // 丢包曲线缩放因子
	Ease easeRate   // 曲线速率生成
	fall float64    // 丢包跌幅累加
	lost int        // 丢包计步累加
	mu   sync.Mutex // 评估&查询分离
}

//
// 新建一个实例。
// 丢包跌幅累计初始化为基础变幅容度。
//
func newLossEval(ease easeRate, zoom float64) *lossEval {
	return &lossEval{
		Ease: ease,
		Zoom: zoom,
		fall: float64(capRange),
	}
}

//
// 是否为丢包模式。
//
func (l *lossEval) Lost() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lost > 0
}

//
// 丢包评估。
// 确认距离越大，丢包概率越大。
// 速率越低，容忍的确认距离越小，反之越大。
// d 为确认距离。
// base 为当前动态基准速率。
//
// 每收到一个确认调用一次。
// 返回true表示一次丢包发生。
//
func (l *lossEval) Eval(d, base float64) bool {
	// 速率容忍
	v := base / float64(BaseRate)
	// 距离关系
	if d*v < lossLimit {
		return false
	}
	l.mu.Lock()
	l.lost += lossStep
	l.fall += float64(lossFall)
	l.mu.Unlock()

	return true
}

//
// 丢包阶段速率。
// 丢包之后的速率为快减方式：先快后缓。
// d 为发送距离。
//
func (l *lossEval) Rate(d, base float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lost > 0 {
		l.lost--
	}
	if l.lost <= 0 {
		l.fall = float64(capRange)
	}
	return time.Duration(l.Ease.Loss(d, base, l.fall, l.Zoom))
}

//
// 缓动速率。
// 算法：Y(x) = Extent * Fn(x) + base
//
// X：横坐标为预发送数据报个数。
// Y：纵坐标为发包间隔时间（越长则越慢）。
//
type easeRate struct {
	ratioCubic
}

//
// 新建一个速率生成器。
// total 为总距离，是速率曲线的基础值。
//
func newEaseRate(total float64) easeRate {
	return easeRate{ratioCubic{total}}
}

//
// 正常发送速率。
// 慢进：先慢后快（指数进度）。
//
// x 为变化因子（可能为负），不局限在底部总值内。
// base 为基准速率（间隔时长）。
// cap 为速率降低的幅度容量。
// zoom 为缩放因子（比率曲线中的 k）。
//
func (s easeRate) Send(x, base, cap, zoom float64) float64 {
	return base + s.UpIn(x, zoom)*cap
}

//
// 丢包后发送速率。
// 快减：很快慢下来，越到后面减速越缓。
//
func (s easeRate) Loss(x, base, cap, zoom float64) float64 {
	return base + s.UpOut(x, zoom)*cap
}

//
// 比率曲线（Easing.Cubic）。
// 用于确定发送距离与发送速率之间的网络适配。
//
// 这是一种主观的尝试，曲线为立方关系（借鉴TCP-CUBIC）。
// 试图适应网络的数据传输及其社会性的拥塞逻辑。
//
type ratioCubic struct {
	Total float64 // 横坐标值总量，曲线基础
}

//
// 慢升-右下弧。
// 从下渐进向上，先慢后快，增量越来越多。
// 注：渐近线在下。
//
// x 为横坐标变量，值应该在总量（rc.Total）之内。
// k 为一个缩放因子，值取1.0上下浮动。
//
func (rc ratioCubic) UpIn(x, k float64) float64 {
	x /= rc.Total
	return x * x * x * k
}

//
// 快升-左上弧。
// 先快后慢，增量越来越少。向上抵达渐进线。
// 注：渐近线在上。
//
func (rc ratioCubic) UpOut(x, k float64) float64 {
	x = x/rc.Total - 1
	return (x*x*x + 1) * k
}
