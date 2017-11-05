// Package goes 并发服务工具集。
// 主要应用在多个Go程对调用宿主（单一）的信号反馈。
package goes

//
// Getter 简单整型索引取值接口。
// 用于按整数索引取值服务。
//
type Getter interface {
	// ok返回false表示结束取值。
	IntGet(i int) (v interface{}, ok bool)
}

//
// Gets 创建一个整型索引取值服务。
//
// 返回的信道用于获取任意类型值，外部需用一个类型断言取值。
// 具体的实现会很清楚获取的是何种类型。
//
// 外部可通过cancel主动退出微服务。
// 注：cancel可由Canceller创建，外部关闭其stop即可。
//
//  @i 通用索引号起始值。
//  @setp 通用索引步进值。
//
func Gets(g Getter, i, step int, cancel func() bool) <-chan interface{} {
	ch := make(chan interface{})

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			val, ok := g.IntGet(i)

			if !ok {
				break
			}
			ch <- val
			i += step
		}
		close(ch)
	}()

	return ch
}

//
// Valuer 通用取值接口。
// 含迭代取键逻辑，比Getter稍复杂但也更灵活。
//
type Valuer interface {
	// 获取值索引键。
	// 返回值用于Value检索取值。
	Key() (k interface{}, ok bool)

	// 获取目标值。
	// 通常，返回一个nil表示无效值（非接口内的nil）。
	Value(k interface{}) interface{}
}

//
// Values 创建一个通用取值服务。
//
func Values(v Valuer, cancel func() bool) <-chan interface{} {
	ch := make(chan interface{})

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			k, ok := v.Key()

			if !ok {
				break
			}
			ch <- v.Value(k)
		}
		close(ch)
	}()

	return ch

}
