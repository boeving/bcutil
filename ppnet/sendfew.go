package ppnet

//////////////////////////////
/// DCP 单数据报发送服务优化版。
///
/// 仅限于尽量小的数据报，最小576b，最大为PMTU值。
/// 主要用于资源请求的发送优化。
///
/// 如果PMTU中途变小使得原数据必须拆分为2个分组以上，
/// 则自动转为普通发送方式。
///
///////////////////////////////////////////////////////////////////////////////

import (
	"log"
	"math/rand"
	"time"

	"github.com/qchen-zh/pputil/goes"
)

// OneAckTime 单数据报确认等待时间。
const OneAckTime = 300 * time.Millisecond

//
// 请求发送器封装。
//
type oneSendReq struct {
	*oneSend
	recv Receiver
}

//
// 启动请求发送服务。
//
func (o *oneSendReq) Serve(re *rateEval, exit *goes.Stop, done reqDoner) {
	o.SetReq()
	o.oneSend.Serve(
		re,
		exit,
		func(id uint16, ack uint32) { done(id, ack, o.recv) },
	)
}

//
// 响应发送器封装。
//
type oneSendRsp struct {
	*oneSend
}

//
// 启动响应发送服务。
//
func (o *oneSendRsp) Serve(re *rateEval, exit *goes.Stop, done rspDoner) {
	o.oneSend.Serve(
		re,
		exit,
		func(id uint16, _ uint32) { done(id) },
	)
}

//
// 单数据报发送器。
// （具体实现）
//
type oneSend struct {
	ID    uint16          // 数据ID#SND
	RcvIn chan<- *rcvInfo // 接收确认信息（<- net）
	iPost chan<- *packet  // 数据报递送（-> xServer）
	iBye  chan<- *ackBye  // 结束通知（BYE -> xServer）
	cLoss chan *ackLoss   // 丢包重发通知（<- AckServe）
	pack  *packet         // 待发送数据报
	isReq bool            // 为请求发送
}

//
// 新建一个子发送服务器。
// seq 实参为一个初始的随机值。
//
func newOneSend(id uint16, seq uint32, b []byte, pch chan<- *packet, bch chan<- ackBye) *oneSend {
	return &oneSend{
		ID:    id,
		RcvIn: make(chan *rcvInfo, 1),
		iPost: pch,
		iBye:  bch,
		pack:  &packet{newHeader(id, seq), b},
	}
}

//
// 启动发送服务。
// 丢包重发仅为简单的超时机制，指数退避。
// 注：
// 初始超时设置稍低，模拟双发保险。
// 因为一个数据报就是一个数据体，冗余浪费可接受。
//
func (o *oneSend) Serve(re *rateEval, exit *goes.Stop, done func(id uint16, ack uint32)) {
	// 初次等待时间
	wait := OneAckTime

	end := time.NewTimer(wait)
	defer end.Stop()

	o.iPost <- o.pack.Init()

	for {
		select {
		// 上级收到BYE后结束
		case <-exit.C:
			return

		// 收到确认，值无关紧要
		case <-o.RcvIn:
			o.toBye(done)
			return

		// 超时重发
		case <-end.C:
			if wait > SendEndtime {
				re.Loss <- struct{}{}
				o.toBye(done)
				log.Printf("wait id:[%d] end ACK timeout.", o.ID)
				return
			}
			o.iPost <- o.pack
			wait += wait
		}
		end.Reset(wait)
	}
}

//
// 设置为资源请求类型。
//
func (o *oneSend) SetReq() {
	o.isReq = true
}

//
// 发送结束通知（BYE）
// 因无数据负载，BYE作为一个确认信息发出。
//
func (o *oneSend) toBye(done sndCleaner) {
	go func() {
		o.iBye <- &ackBye{
			ID:  o.ID,
			Ack: rand.Uint32(),
			Req: o.isReq,
		}
	}()
	done(o.ID, roundPlus(o.ID, o.pack.Size()))
}

//
// 设置基本标记。
//
func (o *oneSend) Init() *packet {
	o.pack.Set(BEG)
	o.pack.Set(END)
	if o.isReq {
		o.pack.Set(REQ)
	}
	return o.pack
}
