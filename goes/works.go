package goes

import (
	"sync"

	"github.com/qchen-zh/pputil"
)

//
// Tasker 任务管理器。
// 用于构造并发任务执行服务。
//  - Task ok返回false表示结束任务分发。
//  - Work 返回非nil表示出错，外部可能回应。
//
type Tasker interface {
	Task() (k interface{}, ok bool)
	Work(k interface{}) error
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
func (l *limitTask) Work(k interface{}) error {
	err := l.tk.Work(k)
	<-l.sema
	return err
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
// 如果有Go程出错，传递首个出错信息后关闭通道，同时不再创建新Go程。
//
// 正常结束后，返回的通道传递nil后关闭。
// 返回的通道保证仅传递一次消息，读取后即无阻塞。
// （未结束的Go程会继续运行到完成，无阻塞）
//
// Task 与 Work 总是成对执行，外部取消不影响这一规则。
//
func Works(t Tasker) <-chan error {
	end := NewCloser()
	stop := make(chan struct{})
	cancel := pputil.Canceller(stop)

	go func() {
		wg := new(sync.WaitGroup)
		for {
			if cancel() {
				break
			}
			v, ok := t.Task()
			if !ok {
				break
			}
			wg.Add(1)

			go func(k interface{}) {
				if err := t.Work(k); err != nil {
					end.Close(err)
					// 容错式关闭
					Close(stop)
				}
				wg.Done()
			}(v)
		}
		wg.Wait()
		// 正常关闭传递nil
		// 容错关闭，无害
		end.Close(nil)
	}()

	return end.C
}

//
// WorksLong 创建并发工作集。
// 用法与Works类似，但由外部控制服务何时终止。
//
// 服务结束之前工作会一直执行（无论是否出错）。
// 每个Go程的出错信息会通过返回的通道持续发送。
// 正常结束或外部主动结束服务后，通道会被直接关闭。
//
// 外部需要清空通道内的值，否则内部Go程会阻塞泄漏。
// cancel可由Canceller创建。
//
func WorksLong(t Tasker, cancel func() bool) <-chan error {
	bad := make(chan error)

	go func() {
		wg := new(sync.WaitGroup)
		for {
			if cancel != nil && cancel() {
				break
			}
			v, ok := t.Task()
			if !ok {
				break
			}
			wg.Add(1)

			go func(v interface{}) {
				if err := t.Work(v); err != nil {
					bad <- err
				}
				wg.Done()
			}(v)
		}
		wg.Wait()
		close(bad)
	}()

	return bad
}
