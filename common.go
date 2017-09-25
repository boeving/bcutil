package pputil

//
// Canceller 取消判断生成器。
// 与传入的stop管道绑定，判断该管道是否关闭。
//
// 常用于服务性Go程根据外部的要求（外部关闭stop），终止自己的服务。
// 通常简单结束即可。避免Go程泄漏。
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
// Closec 安全关闭信号通道。
// 回避重复关闭导致的panic，主要应用于多对一的通知。
// 如多个协程中一个出错，其它协程应当放弃工作。
//
func Closec(ch chan error, msg error) {
	defer func() {
		recover()
	}()
	ch <- msg
	close(ch)
}
