// Package tick 一个容忍零值的定时器（time.Ticker的简单封装）。
// 零值间隔时，Tick方法立即返回一个零值的时间值。
package tick

import "time"

//
// Ticker 容错定时器。
//
type Ticker struct {
	tick *time.Ticker
}

//
// NewTicker 新建一个定时器。
// t 参数可为零值。
//
func NewTicker(t time.Duration) *Ticker {
	if t == 0 {
		return &Ticker{}
	}
	return &Ticker{tick: time.NewTicker(t)}
}

//
// Stop 终止定时器。
//
func (t *Ticker) Stop() {
	if t.tick != nil {
		t.tick.Stop()
	}
}

//
// Tick 按设定的延迟返回一个当前时间值（阻塞）。
// 零延迟时返回一个零值时间。
//
func (t *Ticker) Tick() time.Time {
	if t.tick == nil {
		return zeroTime
	}
	return <-t.tick.C
}

//
// Chan 返回定时器的读取信道。
// 如果间隔时间为零，返回一个非阻塞的已关闭信道。
//
func (t *Ticker) Chan() <-chan time.Time {
	if t.tick == nil {
		return zeroChan
	}
	return t.tick.C
}

// 通用的零值。
var (
	zeroTime = time.Time{}
	zeroChan = make(chan time.Time)
)

func init() {
	close(zeroChan)
}
