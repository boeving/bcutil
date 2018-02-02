package ppnet

import (
	"errors"
	"math/rand"
	"sync"
)

//////////////
/// 流程开始：
/// 	(A)
/// 	Request|xSender => 分配请求数据体ID，创建recvServ实例（等待接收）。
/// 	>>>>>>
/// 	(B)
/// 	service => 响应请求，创建servSend实例 => xServer 发送响应数据。
/// 	>>>>>>
/// 	(A)
/// 	service => 接收响应数据，传递到recvServ实例（已创建） => 应用接收器。
///
/// 流程结束：
/// 	(B)
/// 	servSend(END)|xSender >>>>>>
/// 	(A)
/// 	service|recvServ(Ack) >>>>>>
/// 	(B)
/// 	servSend(BYE, Exit)|xSender(Clean) >>>>>>
/// 	(A)
/// 	service(Clean)|recvServ(Exit, Timeout)
///
///////////////////////////////////////////////////////////////////////////////

var (
	errRecvServ = errors.New("not enough id resources")
)

//
// 发送/接收子服务管理。
// 一个4元组两端连系对应一个本类实例。
//
type dcps struct {
	*forSend                      // servSend 创建参考
	*forAcks                      // recvServ 创建参考
	idx      uint16               // 最新请求ID（数据体ID）存储
	sPool    map[uint16]*servSend // 响应发送子服务（key:#SND）
	rPool    map[uint16]*recvServ // 数据接收子服务（key:#RCV）
	qPool    map[uint16]*servSend // 资源请求发送子服务
	mu       sync.Mutex           // 集合保护（3 map）
}

//
// 新建一个子服务管理器。
// 初始id为一个随机值。
//
func newDcps() *dcps {
	return &dcps{
		forSend: newForSend(),
		forAcks: newForAcks(),
		idx:     uint16(rand.Intn(xLimit16)),
		sPool:   make(map[uint16]*servSend),
		rPool:   make(map[uint16]*recvServ),
		qPool:   make(map[uint16]*servSend),
	}
}

//
// 创建一个接收服务器。
// 返回分配的数据体ID（用于设置请求数据报的发送ID）。
// 如果没有可用的ID资源，返回一个无效值（0xffff）和一个错误提示。
//
func (d *dcps) NewRecvServ() (uint16, error) {
	i := d.reqID(d.idx)
	if i == xLimit16 {
		return i, errRecvServ
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	d.rPool[i] = newRecvServ(i, d.forAcks)
	d.idx = i
	return i, nil
}

//
// 清除数据ID。
// 通常在收到BYE消息时，或END确认发送后超时时被调用。
// 注记：
// 仅需清除recvServ存储即可（servSend的ID为依赖关系）。
//
func (d *dcps) Clean(id uint16) {
	d.mu.Lock()
	delete(d.rPool, id)
	d.mu.Unlock()
}

//
// 返回数据ID的接收子服务器。
//
func (d *dcps) RecvServ(id uint16) *recvServ {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rPool[id]
}

//
// 创建一个发送服务。
// id 由对端的资源请求传递过来。
// 本方法由service实例接收到一个资源请求时调用。
//
func (d *dcps) NewServSend(id uint16, seq uint32, rsp *response) *servSend {
	ss := newServSend(id, seq, rsp, d.forSend)

	d.mu.Lock()
	d.sPool[uint16(id)] = ss
	d.mu.Unlock()

	return ss
}

//
// 创建一个资源请求的发送子服务。
// id 由 NewRecvServ 返回的新分配值指定。
// rsp 通常由一个[]byte构造而来（bytes.Reader）。
// 注：
// 单个数据报的资源请求无需创建此服务。
//
func (d *dcps) ReqServSend(id uint16, seq uint32, rsp *response) *servSend {
	ss := newServSend(id, seq, rsp, d.forSend)
	ss.ReqFlag()

	d.mu.Lock()
	d.qPool[uint16(id)] = ss
	d.mu.Unlock()

	return ss
}

//
// 响应/请求发送完成。
// 在响应发送完成之后（BYE发出）调用。
//
func (d *dcps) Done(id uint16, req bool) {
	d.mu.Lock()
	if req {
		delete(d.qPool, id)
	} else {
		delete(d.sPool, id)
	}
	d.mu.Unlock()
}

//
// 返回数据ID的发送子服务器。
// req 标识是否为资源请求的发送子服务。
//
func (d *dcps) ServSend(id uint16, req bool) *servSend {
	d.mu.Lock()
	defer d.mu.Unlock()
	if req {
		return d.qPool[id]
	}
	return d.sPool[id]
}

//
// 工具函数
///////////////////////////////////////////////////////////////////////////////

//
// 查询获取离id实参值最近的有效ID。
// 如果空位被用完，会执行一次清理。
// 返回0xffff为一个无效值，表示无资源可回收。
//
func (d *dcps) reqID(id uint16) uint16 {
	// 空位
	for i := 0; i < xLimit16; i++ {
		id = uint16(roundPlus2(id, 1))
		if _, ok := d.rPool[id]; !ok {
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
		id = uint16(roundPlus2(id, 1))
		if d.rPool[id].Alive() {
			continue
		}
		if n == xLimit16 {
			n = id // first its
		}
		delete(d.rPool, uint16(id))
	}
	return n
}
