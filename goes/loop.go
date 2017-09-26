package goes

import (
	"sync"

	"github.com/qchen-zh/pputil"
)

//
// Looper 并发循环器。
// 用于从循环构造并发工作服务器。
//  - Next返回nil时退出循环；
//  - Work返回error表示出错，外部可能终止循环；
//
type Looper interface {
	Next() interface{}
	Work(k interface{}) error
}

//
// limitLoop 有效并发循环器。
//
type limitLoop struct {
	lp   Looper
	sema chan struct{}
}

//
// Next 有限性递进。
//
func (l *limitLoop) Next() interface{} {
	l.sema <- struct{}{}
	return l.lp.Next()
}

//
// Work 工作执行覆盖。
//
func (l *limitLoop) Work(k interface{}) error {
	err := l.lp.Work(k)
	<-l.sema
	return err
}

//
// LimitLooper 创建有限并发循环器。
//
func LimitLooper(lp Looper, limit int) Looper {
	ll := limitLoop{
		lp,
		make(chan struct{}, limit),
	}
	return &ll
}

//
// LoopWorks 并发工作服务。
// 针对每一次循环（lp.Next），对lp.Work开启一个单独的Go程执行。
//
// 外部通过bad获得lp.Work的执行状态（出错信息），
// 通过cancel可以主动控制内部服务退出。
// 注：cancel可由Canceller创建，外部关闭其stop即可传递结束信号。
//
// 正常结束和外部主动结束服务，bad管道传递nil后关闭。
//
func LoopWorks(lp Looper, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	wg := new(sync.WaitGroup)
	wg.Add(1)

	go func() {
		for {
			if cancel != nil && cancel() {
				break
			}
			v := lp.Next()
			if v == nil {
				break
			}
			wg.Add(1)
			go func(v) {
				if err := lp.Work(v); err != nil {
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
