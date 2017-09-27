package goes

import (
	"sync"

	"github.com/qchen-zh/pputil"
)

//
// Worker 并发工作器。
// 用于构造并发工作服务集。
//  - Next返回nil时结束并发创建；
//  - Work返回error表示出错，外部可能终止服务；
//
type Worker interface {
	Next() interface{}
	Work(k interface{}) error
}

//
// limitWork 有限并发工作器。
//
type limitWork struct {
	wk   Worker
	sema chan struct{}
}

//
// Next 有限性迭代取值。
//
func (l *limitWork) Next() interface{} {
	l.sema <- struct{}{}
	return l.wk.Next()
}

//
// Work 工作执行覆盖。
//
func (l *limitWork) Work(k interface{}) error {
	err := l.wk.Work(k)
	<-l.sema
	return err
}

//
// LimitWorker 创建有限并发工作器。
//
func LimitWorker(w Worker, limit int) Worker {
	lw := limitWork{
		w,
		make(chan struct{}, limit),
	}
	return &lw
}

//
// Works 创建并发工作集。
// 针对每一次迭代（w.Next），对lp.Work开启一个单独的Go程执行。
//
// 外部通过bad获得lp.Work的执行状态（出错信息），
// 通过cancel可以主动控制内部服务退出。
// 注：cancel可由Canceller创建，外部关闭其stop即可传递结束信号。
//
// 正常结束和外部主动结束服务，bad管道传递nil后关闭。
//
func Works(w Worker, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	wg := new(sync.WaitGroup)
	wg.Add(1)

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			v := w.Next()
			if v == nil {
				break
			}
			wg.Add(1)
			go func(v) {
				if err := w.Work(v); err != nil {
					pputil.Closec(bad, err)
				}
				wg.Done()
			}(v)
		}
		pputil.Closec(bad, nil)
		wg.Done()
	}()

	return wg
}
