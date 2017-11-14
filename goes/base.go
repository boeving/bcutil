package goes

//
// Canceller 取消判断生成器。
// 与传入的stop管道绑定，判断该管道是否关闭。
//
// 常用于服务性Go程，由外部关闭stop管道来终止自己的服务。
//
func Canceller(stop <-chan struct{}) func() bool {
	return func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
}

//
// Send 容错式发送信息。
//
// 兼容管道关闭后继续发送信息的情况（屏蔽panic）。
// 容许ch值为nil，静默返回（与标准行为不同：除非被读取阻塞，否则总会返回）。
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
// 回避重复关闭导致的panic，应用场景与Send类似。
// 兼容ch值为nil，静默返回。
//
// 外部仍需将ch内的信号清空，以防关闭前有Go程阻塞。
// 如果“一旦出错就应该关闭”，则使用Closer更便捷。
//
// 除非是明确回避多次关闭的panic，否在应避免使用。暗藏的多次关闭可能有逻辑上的问题。
//
func Close(ch chan<- struct{}) {
	if ch == nil {
		return
	}
	defer func() { recover() }()
	close(ch)
}

//
// Stop 停止控制器。
//
type Stop struct {
	C chan struct{}
}

//
// NewStop 创建一个停止控制器。
//
func NewStop() *Stop {
	return &Stop{make(chan struct{})}
}

//
// Off 执行停止。
// 调用之后实例不再可用。
//
func (s *Stop) Off() {
	close(s.C)
}

//
// Sema 信号控制器。
// 用于协调多个协程之间的退出控制（并发安全）。
// 可以是一对一，或一对多。
// 注：
// 功能类似标准库context的WithCancel。但无继承逻辑（极简版）。
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
		fn: Canceller(c),
	}
}

//
// Off 结束信号。
// 多次结束不会有更多的效果（静默容错）。
//
func (s *Sema) Off() {
	Close(s.ch)
}

//
// Offed 是否已终止。
//
func (s *Sema) Offed() bool {
	return s.fn()
}

//
// Done 返回信号通道。
// 常用于select中监控退出信号。
// 应直接读取而非保存到一个外部变量，否则On之后变量值会失效。
// 如：
//  select {
//  case <-x.Done(): ...
//
func (s *Sema) Done() <-chan struct{} {
	return s.ch
}

//
// On 信号器重启。
// 实际上为创建一个新的信号器并原地赋值。
// （注：不常用，通常Off之后逻辑已经完成）
//
func (s *Sema) On() {
	*s = *NewSema()
}
