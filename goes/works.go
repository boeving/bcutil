package goes

import (
	"sync"

	"github.com/qchen-zh/pputil/goes"
)

//
// Tasker 任务管理器。
// 用于构造并发任务执行服务。
//  - Task 返回nil时结束并发创建；
//  - Work 返回error表示出错，外部可以终止服务；
//
type Tasker interface {
	Task() interface{}
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
func (l *limitTask) Task() interface{} {
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
// 外部通过bad获得t.Work的执行状态（出错信息），
// 然后可以通过cancel主动控制内部服务退出。
//
// 正常结束或外部主动结束服务后，bad管道关闭。
// 外部应用始终需要清空bad，以防Go程泄漏，
// 同时，返回的WaitGroup也才会正常归零。
//
// 注：cancel可由Canceller创建，外部关闭其stop即可传递结束信号。
//
func Works(t Tasker, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	wg := new(sync.WaitGroup)
	wg.Add(1)

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			v := t.Task()
			if v == nil {
				break
			}
			wg.Add(1)

			go func(v interface{}) {
				if err := t.Work(v); err != nil {
					goes.Send(bad, err)
				}
				wg.Done()
			}(v)
		}
		close(bad)
		wg.Done()
	}()

	return wg
}
