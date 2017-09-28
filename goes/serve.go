// Package goes 通用并发类服务工具集。
package goes

//
// Valuer 取值接口。
// 用于微服务的取值传递。
//
type Valuer interface {
	// ok返回false表示结束取值。
	Value() (v interface{}, ok bool)
}

//
// Serve 创建一个通用取值服务。
//
// 返回的管道用于获取任意类型值，外部需用一个类型断言取值。
// 具体的类型通常在调用接口处说明。
//
// 外部可通过cancel主动退出微服务。
// 注：cancel可由Canceller创建，外部关闭其stop即可。
//
func Serve(v Valuer, cancel func() bool) <-chan interface{} {
	ch := make(chan interface{})

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			val, ok := v.Value()

			if !ok {
				break
			}
			ch <- val
		}
		close(ch)
	}()

	return ch
}
