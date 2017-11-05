package goes

import (
	"sync"
)

//
// Tasker 任务分发器。
// 用于构造并发任务执行服务。
//  - Task ok返回false表示结束任务分发。
//  - Work 返回非nil表示出错，外部可能回应或忽略。
//
type Tasker interface {
	Task() (k interface{}, ok bool)
	Work(k interface{}, sm *Sema) error
}

//
// limitTask 有限并发任务。
//
type limitTask struct {
	tk   Tasker
	sema chan struct{}
}

//
// Task 延迟控制获取任务。
//
func (l *limitTask) Task() (interface{}, bool) {
	l.sema <- struct{}{}
	return l.tk.Task()
}

//
// Work 工作执行覆盖。
//
func (l *limitTask) Work(k interface{}, sm *Sema) error {
	defer func() {
		<-l.sema
	}()
	return l.tk.Work(k, sm)
}

//
// LimitTasker 创建一个有限并发任务管理器。
//
func LimitTasker(t Tasker, limit int) Tasker {
	if limit <= 0 {
		return nil
	}
	lt := limitTask{
		t,
		make(chan struct{}, limit),
	}
	return &lt
}

//
// Works 创建并发工作集。
//
// 针对每一次迭代（t.Task），对t.Work开启一个单独的Go程执行。
// 返回的通道用于获得t.Work的执行状态。
// 如果有Go程出错，传递出错信息，同时不再创建新Go程。
// 未结束的Go程会无阻塞继续运行到完成。
//
// 适用于很多工作只要一个出错就可终止的场景，
// 通常用LimitTasker限定，因为Go程的创建很快，Work的错误来不及中断Go程的创建过程。
// ——除非任务（Task）本身有阻塞逻辑。
//
// 正常结束后，返回的通道发送随机的一次出错信息（如果有）。
// 外部通过对返回通道的读取（单次），等待内部工作全部结束。
//
// Task 与 Work 总是成对执行，出错后的退出不影响这一规则。
//
func Works(t Tasker) <-chan error {
	bad := make(chan error)
	sem := NewSema()

	go func() {
		var (
			wg sync.WaitGroup
			ge error
		)
		for {
			if sem.Offed() {
				break
			}
			v, ok := t.Task()
			if !ok {
				break
			}
			wg.Add(1)

			go func(k interface{}) {
				defer wg.Done()

				if err := t.Work(k, sem); err != nil {
					sem.Off()
					// 只写，竞争
					ge = err
				}
			}(v)
		}
		wg.Wait()
		// 同步后，其中一个错误或nil
		bad <- ge
	}()

	return bad
}

//
// WorksLong 创建并发工作集。
// 用法与Works类似，但会一直工作并持续传递可能有的出错信息，直到被主动结束。
// 工作结束后（所有协程运行完毕），通道会被关闭。
//
// 适用于很多工作中容许部分工作失效（后续有效依然可行）的场景。
// 如：文件的分片下载/存储，失效部分被收集重做。
//
// 外部需要持续读取通道，否则通道不会关闭，内部协程也会阻塞泄漏。
// 如果外部需要提前结束服务，可提前结束其Task或在Work中调用sm.Off()。
//
func WorksLong(t Tasker, sm *Sema) <-chan error {
	bad := make(chan error)

	go func() {
		var wg sync.WaitGroup
		for {
			if sm.Offed() {
				break
			}
			v, ok := t.Task()
			if !ok {
				break
			}
			wg.Add(1)

			go func(v interface{}) {
				defer wg.Done()

				if err := t.Work(v, sm); err != nil {
					bad <- err
				}
			}(v)
		}
		wg.Wait()
		close(bad)
	}()

	return bad
}

//
// Firster 首先成功者。
//
type Firster interface {
	Task() (k interface{}, ok bool)
	First(k interface{}, sm *Sema) (v interface{}, ok bool)
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
func (l *limitFirst) First(k interface{}, sm *Sema) (interface{}, bool) {
	defer func() {
		<-l.sema
	}()
	return l.ft.First(k, sm)
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
// WorksFirst 仅获取首个成功执行的结果。
// 应用：如对多个镜像网站相同目标的数据请求。
//
func WorksFirst(f Firster) interface{} {
	vch := make(chan interface{})
	sem := NewSema()

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
				if v, ok := f.First(v, sem); ok {
					select {
					case vch <- v:
						sem.Off()
					default:
					}
				}
			}(v)
		}
	}()

	return <-vch
}
