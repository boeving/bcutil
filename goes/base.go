package goes

import "sync"

//
// Send 容错式发送信息。
//
// 兼容管道关闭后继续发送信息的情况（屏蔽panic）。
// 容许ch值为nil，静默返回（无任何效果）。
//
// 主要应用于多对一的情形，
// 例如多个协程中一个出错导致通道关闭，而其它协程仍未执行结束。
//
func Send(ch chan<- error, msg error) {
	if ch == nil {
		return
	}
	defer func() { recover() }()
	ch <- msg
}

//
// Close 容错式关闭信号通道。
//
// 回避重复关闭导致的panic，应用场景与Sendc类似。
// 兼容ch值为nil，静默返回。
//
func Close(ch chan<- error) {
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
//
type Closer struct {
	ch chan<- error
	mu sysc.Mutex
}

//
// NewCloser 创建一个关闭信号器。
//
func NewCloser(ch chan<- error) *Closer {
	return &Closer{
		ch: ch,
		mu: sync.Mutex,
	}
}

//
// Close 发送并关闭。
// 仅发送一条消息即关闭通道，后续调用静默容错。
//
// 通常，应用仅在出错时才调用此方法。
//
func (c *Closer) Close(msg error) {
	defer func() {
		recover()
		c.mu.Unlock()
	}()
	c.mu.Lock()

	c.ch <- msg
	close(c.ch)
}
