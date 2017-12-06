package goes

import (
	"sync"
)

//
// Worker 并发工作器。
// 获得一个任务（Task）之后，新建一个Go程执行Work。
//
// 两个接口中都会传递一个over函数，用于判断是否被外部结束。
// 接口的实现中可据此决定是否或何时中途退出。
//
// 这有点像现实中的工厂工作，通知结束后，工人自行决定手上的活儿。
// 在Work中，通常在一些关键节点处判断。
//
type Worker interface {
	// 任务分发，ok返回false表示分发结束。
	// over为通知结束的判断函数。
	Task(over func() bool) (k interface{}, ok bool)

	// over 判断外部是否已取消工作。
	// 返回非nil表示出错，由外部执行处理逻辑或忽略。
	Work(k interface{}, over func() bool) error
}

//
// limitTask 有限并发工作器。
//
type limitTask struct {
	w   Worker
	sem chan struct{}
}

//
// Task 延迟控制获取任务。
//
func (l *limitTask) Task(over func() bool) (interface{}, bool) {
	l.sem <- struct{}{}
	return l.w.Task(over)
}

//
// Work 工作执行覆盖。
//
func (l *limitTask) Work(k interface{}, over func() bool) error {
	defer func() {
		<-l.sem
	}()
	return l.w.Work(k, over)
}

//
// LimitWorker 创建一个有限并发工作器。
//
func LimitWorker(w Worker, limit int) Worker {
	if limit <= 0 {
		return nil
	}
	lt := limitTask{
		w,
		make(chan struct{}, limit),
	}
	return &lt
}

//
// Works 并发阻塞直到工作结束。
// 有一个出错即不再创建新的工作，但会等待已经开始的工作结束。
// 返回首个出错的信息。
//
// 适用于很多工作只要一个出错就可终止的场景。
// 通常用LimitTasker限定，除非任务（Task）本身有阻塞逻辑。
//
// 出错后仅写入一个错误信息，但不保证是首个返回的错误信息。
// 外部读取返回通道的值（单次即可），阻塞等待内部工作全部结束。
//
func Works(w Worker) error {
	bad := make(chan error)
	sem := NewSema()

	go func() {
		var wg sync.WaitGroup
		for {
			if sem.Offed() {
				break
			}
			v, ok := w.Task(sem.Fn())
			if !ok {
				break
			}
			wg.Add(1)

			go func(k interface{}) {
				defer wg.Done()

				if err := w.Work(k, sem.Fn()); err != nil {
					sem.Off()
					bad <- err
				}
			}(v)
		}
		wg.Wait()
		close(bad) // 无阻塞关闭
	}()

	rv := <-bad
	if rv != nil {
		// 等待其它Go程结束
		for _ = range bad {
		}
	}
	return rv
}

//
// WorksLong 持续工作，直到外部主动结束。
// 持续传递可能有的出错信息，所有Go程结束，通道会被关闭。
//
// 适用于很多工作中容许部分工作失效（后续有效依然可行）的场景。
// 如：文件的分片下载/存储，失效部分被收集重做。
//
// 外部需要持续读取通道以避免内部阻塞，否则通道不会关闭。
// 注：外部提前结束通过 sem.Off() 实施。
//
func WorksLong(w Worker, sem *Sema) <-chan error {
	bad := make(chan error)
	// 占位简化
	if sem == nil {
		sem = longSem
	}
	go func() {
		var wg sync.WaitGroup
		for {
			if sem.Offed() {
				break
			}
			v, ok := w.Task(sem.Fn())
			if !ok {
				break
			}
			wg.Add(1)

			go func(v interface{}) {
				defer wg.Done()

				if err := w.Work(v, sem.Fn()); err != nil {
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
// 长信号，永不关闭。
// 用于占位，简化编码。
// 注：本库编码中调用者注意不可关闭！
//
var longSem = NewSema()
