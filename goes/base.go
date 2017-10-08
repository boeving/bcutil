package goes

import (
	"sync"

	"github.com/qchen-zh/pputil"
)

//
// Send 容错式发送信息。
//
// 兼容管道关闭后继续发送信息的情况（屏蔽panic）。
// 容许ch值为nil，静默返回（无任何效果）。
//
// 主要应用于多对一的情形，
// 例如多个Go程中一个出错导致通道关闭，而其它Go程仍未执行结束。
//
func Send(ch chan<- error, err error) {
	if ch == nil {
		return
	}
	defer func() { recover() }()
	ch <- err
}

//
// Close 容错式关闭信号通道。
//
// 回避重复关闭导致的panic，应用场景与Send类似。
// 兼容ch值为nil，静默返回。
//
// 外部仍需将ch内的信号清空，以防关闭前有Go程阻塞。
// 如果“一旦出错就应该关闭”，则使用Closer更便捷。
//
func Close(ch chan<- struct{}) {
	if ch == nil {
		return
	}
	defer func() { recover() }()
	close(ch)
}

//
// Closer 关闭信号器。
// 保证单次信号发送后关闭通道。
//
// 应用场景为多对一时的出错通知（后续关闭操作静默容错）。
// 保证通道即时关闭，
// 外部应用仅需读取一次通道即可，无并发协程阻塞泄漏。
//
type Closer struct {
	C  chan error
	mu sync.Mutex
}

//
// NewCloser 创建一个关闭信号器。
//
func NewCloser() *Closer {
	return &Closer{C: make(chan error)}
}

//
// Close 发送并关闭。
// 仅发送一条消息即关闭通道，后续调用静默容错，无阻塞。
//
// 通常，应用仅在出错时才调用此方法。
//
func (c *Closer) Close(err error) {
	defer func() {
		recover()
		c.mu.Unlock()
	}()
	c.mu.Lock()

	c.C <- err
	close(c.C)
}

//
// Sema 信号控制器。
// 用于协调多个协程之间的退出控制。
// 可以是一对一，或一对多。
//
type Sema struct {
	ch chan struct{}
	fn func() bool
}

//
// NewSema 新建一个信号量控制器。
//
func NewSema() *Sema {
	c := make(chan struct{})
	return &Sema{
		ch: c,
		fn: pputil.Canceller(c),
	}
}

//
// Exit 结束信号。
// 多次结束不会有更多的效果（静默容错）。
//
func (s *Sema) Exit() {
	Close(s.ch)
}

//
// Dead 信号是否已失效。
//
func (s *Sema) Dead() bool {
	return s.fn()
}

//
// Sema 返回信号通道。
// 一般用于select中监控退出信号。
//
func (s *Sema) Sema() <-chan struct{} {
	return s.ch
}

//
// Renew 信号重新开始。
// 如果之前的信号未关闭，会先关闭。
// 通常很少需要此接口。
//
func (s *Sema) Renew() {
	Close(s.ch)
	s.ch = make(chan struct{})
	s.fn = pputil.Canceller(s.ch)
}
