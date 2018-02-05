package ppnet

import (
	"bytes"
	"errors"
	"math/rand"
	"sync"
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
	reqSend  map[uint16]*servSend // 资源请求发送子服务（key:next）
	rspRecv  map[uint16]*recvServ // 响应接收子服务（key:next）
	reqRecv  map[uint16]*recvServ // 请求接收子服务（key:net #SND->#RCV）
	rspSend  map[uint16]*servSend // 响应发送子服务（key:net #SND->#SND）
	mu       sync.Mutex           // 集合保护（4 map）
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
		rspRecv: make(map[uint16]*recvServ),
		reqRecv: make(map[uint16]*recvServ),
		rspSend: make(map[uint16]*servSend),
		reqSend: make(map[uint16]*servSend),
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
// 新建一个请求关联服务。
// 包含请求发送子服务和对应的响应接收子服务。
// 返回新建的两个子服务。
//
func (d *dcps) NewRequest(res []byte) (*servSend, *recvServ, error) {
	i := d.reqID(d.idx)
	if i == xLimit16 {
		return nil, nil, errRecvServ
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	rs := newRecvServ(i, d.forAcks)
	d.rspRecv[i] = rs

	ss := newServSend(
		i,
		rand.Uint32()%xLimit32,
		newResponse(bytes.NewReader(res)),
		d.forSend,
	)
	d.reqSend[i] = ss

	d.idx = i
	return ss, rs, nil
}

//
// 清除数据ID。
// 通常在收到BYE消息时，或END确认发送后超时时被调用。
// 注记：
// 仅需清除recvServ存储即可（servSend的ID为依赖关系）。
//
func (d *dcps) Clean(id uint16, req bool) {
	d.mu.Lock()
	if req {
		delete(d.reqRecv, id)
	} else {
		delete(d.rspRecv, id)
	}
	d.mu.Unlock()
}

//
// 返回数据ID的接收子服务器。
//
func (d *dcps) RecvServ(id uint16) *recvServ {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rspRecv[id]
}

//
// 创建一个请求接收子服务。
//
func (d *dcps) NewRecvServ(id uint16) *recvServ {
	rs := newRecvServ(id, d.forAcks)

	d.mu.Lock()
	d.reqRecv[id] = rs
	d.mu.Unlock()

	return rs
}

//
// 创建一个响应发送服务。
// 由service实例接收到一个资源请求时调用。
// 注：id由对端的资源请求传递过来。
//
func (d *dcps) NewServSend(id uint16, rsp *response) *servSend {
	ss := newServSend(
		id,
		rand.Uint32()%xLimit32,
		rsp,
		d.forSend,
	)
	d.mu.Lock()
	d.rspSend[uint16(id)] = ss
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
		delete(d.reqSend, id)
	} else {
		delete(d.rspSend, id)
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
		return d.reqSend[id]
	}
	return d.rspSend[id]
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
		id = uint16(roundPlus2(id, 1))
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
		id = uint16(roundPlus2(id, 1))
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
