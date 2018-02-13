package ppnet

import (
	"bytes"
	"errors"
	"math/rand"
	"sync"

	"github.com/qchen-zh/pputil/goes"
)

//////////////
/// 流程开始：
/// 	(A)
/// 	Request|xSender => 分配请求数据体ID，
/// 	创建请求发送子服务（servSend）和响应接收子服务（recvServ）。
/// 	>>>>>>
/// 	(B)
/// 	service => 创建接收请求子服务（recvServ），
/// 	接收完毕后，创建响应发送子服务（servSend），发送响应 => xServer。
/// 	>>>>>>
/// 	(A)
/// 	service => 已创建的recvServ接收响应数据 => 应用接收器。
///
/// 流程结束：
/// 	(B)
/// 	最后一个数据报：servSend(END)|xSender >>>>>>
/// 	(A)
/// 	service|recvServ(Ack) 确认最后一个数据报 >>>>>>
/// 	(B)
/// 	servSend(BYE, Exit)|xSender(Done) >>>>>>
/// 	(A)
/// 	service(Clean)|recvServ(Exit, Timeout)
///
///////////////////////////////////////////////////////////////////////////////

var errRequest = errors.New("not enough id resources")

//
// 资源请求写入缓存。
//
type resBuffer struct {
	*bytes.Buffer
	done func()
}

//
// Receiver 接口实现。
//
func (rb *resBuffer) Close() error {
	rb.done()
	return nil
}

func newResBuffer(done func()) *resBuffer {
	return &resBuffer{new(bytes.Buffer), done}
}

//
// 请求发送完成回调。
// 需要创建一个响应接收子服务。
//
type reqDoner func(id uint16, ack uint32, rc Receiver)

//
// 请求接收结束清理回调。
// 需要创建一个响应发送子服务。
//
type reqCleaner func(id uint16, seq uint32, rp responser)

//
// 响应发送完成回调。
//
type rspDoner func(id uint16)

//
// 响应接收结束清理回调。
//
type rspCleaner func(id uint16)

//
// 请求发送接口。
//
type reqSender interface {
	Serve(*rateEval, *goes.Stop, reqDoner)
}

//
// 请求接收接口。
//
type reqReceiver interface {
	Serve(*goes.Stop, reqCleaner)
}

//
// 响应发送接口。
//
type rspSender interface {
	Serve(*rateEval, *goes.Stop, rspDoner)
}

//
// 响应接收接口。
//
type rspReceiver interface {
	Serve(*goes.Stop, rspCleaner)
}

//
// 发送/接收子服务管理。
// 一个4元组两端连系对应一个本类实例。
//
type dcps struct {
	*forSend                        // 发送相关信道存储
	idx      uint16                 // 最新请求ID（数据体ID）存储
	reqSend  map[uint16]reqSender   // 资源请求发送子服务（key:next）
	reqRecv  map[uint16]reqReceiver // 请求接收子服务（key:net #SND->#RCV）
	rspSend  map[uint16]rspSender   // 响应发送子服务（key:net #SND->#SND）
	rspRecv  map[uint16]rspReceiver // 响应接收子服务（key:next）
	mu       sync.Mutex             // 集合保护（4 map）
}

//
// 新建一个子服务管理器。
// 初始id为一个随机值。
//
func newDcps() *dcps {
	return &dcps{
		forSend: newForSend(),
		idx:     uint16(rand.Intn(xLimit16)),
		reqSend: make(map[uint16]reqSender),
		reqRecv: make(map[uint16]reqReceiver),
		rspSend: make(map[uint16]rspSender),
		rspRecv: make(map[uint16]rspReceiver),
	}
}

//
// 请求池大小（请求数量）。
//
func (d *dcps) ReqSize() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.reqSend)
}

//
// 创建一个请求发送子服务。
// 如果内部ID资源不足，则返回一个errRequest错误。
//
func (d *dcps) NewRequest(res []byte) (reqSender, error) {
	i := d.reqID(d.idx)
	if i == xLimit16 {
		return nil, errRequest
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	ss := newServSend(
		i,
		rand.Uint32()%xLimit32,
		newResponse(bytes.NewReader(res)),
		true,
		d.Post,
		d.Bye,
		d.Dist,
	)
	d.reqSend[i] = ss
	d.idx = i

	return ss, nil
}

//
// 创建一个响应接收子服务。
// 它通常在一个请求发送结束后被调用。
// ack 为响应端将要发送的首个分组的序列号（约定）。
//
func (d *dcps) NewReceive(id uint16, ack uint32, rc Receiver) rspReceiver {
	rs := newRecvServ(id, ack, d.AckReq, rc, false)

	d.mu.Lock()
	d.rspRecv[id] = rs
	d.mu.Unlock()

	return rs
}

//
// 创建一个请求接收子服务器。
// id 由资源请求数据报传递过来。
// done 为请求接收完毕后的调用（用于创建响应发送）。
//
func (d *dcps) NewReqReceiver(id uint16, ack uint32, done func()) reqReceiver {
	d.mu.Lock()
	defer d.mu.Unlock()

	rs := newRecvServ(id, ack, d.AckReq, newResBuffer(done), true)
	d.reqRecv[id] = rs

	return rs
}

//
// 创建一个响应发送子服务。
// 它在一个资源请求接收完毕后被调用（NewReceive的回调里）。
//
// id 由资源请求数据报传递过来。
// seq 为资源请求最后分组的确认号（约定）。
//
func (d *dcps) NewRspSender(id uint16, seq uint32, rsp *response) rspSender {
	ss := newServSend(
		id, seq, rsp, false,
		d.Post, d.Bye, d.Dist,
	)
	d.mu.Lock()
	d.rspSend[uint16(id)] = ss
	d.mu.Unlock()

	return ss
}

//
// 请求发送完成。
// 注：会在BYE发送之后被调用。
//
func (d *dcps) ReqDone(id uint16) {
	d.mu.Lock()
	delete(d.reqSend, id)
	d.mu.Unlock()
}

//
// 发送完成（请求/响应）。
// 在BYE发送之后被调用。
//
func (d *dcps) RspDone(id uint16) {
	d.mu.Lock()
	delete(d.rspSend, id)
	d.mu.Unlock()
}

//
// 清理请求接收池。
// 注：通常在收到BYE或重复END确认超时被调用。
//
func (d *dcps) ReqClean(id uint16) {
	d.mu.Lock()
	delete(d.reqRecv, id)
	d.mu.Unlock()
}

//
// 清理响应接收池。
// 注：通常在收到BYE或重复END确认超时被调用。
//
func (d *dcps) RspClean(id uint16) {
	d.mu.Lock()
	delete(d.rspRecv, id)
	d.mu.Unlock()
}

//
// 获取请求发送子服务器。
//
func (d *dcps) ReqSender(id uint16) reqSender {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reqSend[id]
}

//
// 获取响应发送子服务器。
//
func (d *dcps) RspSender(id uint16) rspSender {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rspSend[id]
}

//
// 获取响应接收子服务器。
//
func (d *dcps) RspReceiver(id uint16) rspReceiver {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rspRecv[id]
}

//
// 工具函数
///////////////////////////////////////////////////////////////////////////////

//
// 查询获取离id实参值最近的有效ID。
// 如果空位被用完，会执行一次清理。
// 返回0xffff为一个无效值，表示无资源可回收。
//
// 注记：
// 从响应接收池中取空闲ID，因为响应接收决定完成情况。
//
func (d *dcps) reqID(id uint16) uint16 {
	// 空位
	for i := 0; i < xLimit16; i++ {
		id = roundPlus2(id, 1)
		if _, ok := d.rspRecv[id]; !ok {
			return id
		}
	}
	// 兼顾性能和存活宽容，只清理1/3。
	return d.recycle(id, 3)
}

//
// 清理不存活的ID，回收资源。
// 如果没有回收资源可用，返回一个无效值0xffff。
// 否则返回第一个回收的值。
//
// lev 为清理等级，1为全部清理，3为三分之一。
//
func (d *dcps) recycle(id uint16, lev int) uint16 {
	var n uint16 = xLimit16

	for i := 0; i < xLimit16/lev; i++ {
		id = roundPlus2(id, 1)
		if d.rspRecv[id].Alive() {
			continue
		}
		if n == xLimit16 {
			n = id // first its
		}
		delete(d.rspRecv, uint16(id))
	}
	return n
}
