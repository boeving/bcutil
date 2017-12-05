package goes

//
// Firster 首先成功者。
//
type Firster interface {
	// 任务分发，与Worker兼容。
	Task() (k interface{}, ok bool)

	// 执行工作并返回结果。
	// 与Worker不同，用更友好的返回值的接口。
	First(k interface{}, over func() bool) (v interface{}, ok bool)
}

//
// limitFirst 有限并发任务。
//
type limitFirst struct {
	ft   Firster
	sema chan struct{}
}

//
// Task 延迟控制获取任务。
//
func (l *limitFirst) Task() (interface{}, bool) {
	l.sema <- struct{}{}
	return l.ft.Task()
}

//
// First 工作执行覆盖。
//
func (l *limitFirst) First(k interface{}, over func() bool) (interface{}, bool) {
	defer func() {
		<-l.sema
	}()
	return l.ft.First(k, over)
}

//
// LimitFirster 创建一个有限并发首值获取器。
//
func LimitFirster(f Firster, limit int) Firster {
	if limit <= 0 {
		return nil
	}
	lf := limitFirst{
		f,
		make(chan struct{}, limit),
	}
	return &lf
}

//
// First 并发阻塞，直到有一个成功。
// 返回首个成功执行的结果。
//
// 应用：如对多个镜像网站相同URL的数据请求。
//
func First(f Firster) interface{} {
	sem := NewSema()
	vch := make(chan interface{})

	go func() {
		for {
			if sem.Offed() {
				break
			}
			v, ok := f.Task()
			if !ok {
				break
			}
			go func(v interface{}) {
				if v, ok := f.First(v, sem.Fn()); ok {
					select {
					case vch <- v:
						sem.Off()
					default:
						// 丢弃后来的成功
					}
				}
			}(v)
		}
	}()

	return <-vch
}
