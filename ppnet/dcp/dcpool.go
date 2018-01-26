package dcp

import "time"

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

//
// 发送/接收子服务管理。
//
type dcpx struct {
	id    int                  // 最新请求ID记录
	sPool map[uint16]*servSend // 发送服务器池（key:#SND）
	rPool map[uint16]*recvServ // 接收服务器池（key:#RCV）
}

//
// 创建一个接收服务器。
// 返回分配的数据体ID（用于设置请求数据报的发送ID）。
// 如果没有可用的ID资源，返回一个无效值（0xffff）和false。
//
func (x *dcpx) NewRecvServ(req chan<- *ackReq, rtp, rack, ack <-chan int) (int, bool) {
	id := x.reqID(x.id)
	if id == xLimit16 {
		return id, false
	}
	x.rPool[uint16(id)] = &recvServ{
		ID:    uint16(id),
		AReq:  req,
		Rtp:   rtp,
		Rack:  rack,
		Ack:   ack,
		alive: time.Now(),
	}
	x.id = id
	return id, true
}

//
// 返回数据ID的接收子服务器。
//
func (x *dcpx) RecvServ(id uint16) *recvServ {
	return x.rPool[id]
}

//
// 创建一个发送服务。
// 发送的数据ID由对端的资源请求传递过来。
//
func (x *dcpx) NewServSend(id uint16, pch chan<- *packet, bye chan<- *ackReq, lch <-chan int, er *evalRate, rp *response, ar *ackRecv) {
	ss := servSend{
		ID:   id,
		Post: pch,
		Bye:  bye,
		Loss: lch,
		Eval: er,
		Resp: rp,
		Recv: ar,
	}
	x.sPool[id] = &ss
}

//
// 返回数据ID的发送子服务器。
//
func (x *dcpx) ServSend(id uint16) *servSend {
	return x.sPool[id]
}

//
// 工具函数
///////////////////////////////////////////////////////////////////////////////

//
// 查询获取离id实参值最近的有效ID。
// 如果空位被用完，会执行一次清理。
// 返回0xffff为一个无效值，表示无资源可回收。
//
func (x *dcpx) reqID(id int) int {
	// 空位
	for i := 0; i < xLimit16; i++ {
		id = round16(id, 1)
		if _, ok := x.rPool[uint16(id)]; !ok {
			return id
		}
	}
	// 兼顾性能和存活宽容，只清理1/3。
	return x.Clean(id, 3)
}

//
// 清理不存活的ID，回收资源。
// 如果没有回收资源可用，返回一个无效值0xffff。
// 否则返回第一个回收的值。
//
// lev 为清理等级，1为全部清理，3为三分之一。
//
func (x *dcpx) Clean(id, lev int) int {
	n := xLimit16

	for i := 0; i < xLimit16/lev; i++ {
		id = round16(id, 1)
		if x.rPool[uint16(id)].Alive() {
			continue
		}
		if n == xLimit16 {
			n = id
		}
		delete(x.rPool, uint16(id))
	}
	return n
}
