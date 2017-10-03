package download

import (
	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/goes"
)

type (
	Piece  = piece.Piece
	Pieces = piece.Pieces
)

const (
	// MaxThread 默认下载协程数
	MaxThread = 8
)

//
// Hauler 数据搬运工。
// 实施单个目标（分片）的具体下载行为，
//
type Hauler interface {
	// 获取数据。
	Get(Piece) ([]byte, error)
}

//
// PieceData 分片数据（用于存储）。
//
type PieceData struct {
	Offset int64
	Bytes  []byte
}

//
// Downloader 下载器。
// 负责目标数据的分片，组合校验，缓存等。
//
type Downloader struct {
	NewHauler func() Hauler  // 创建数据搬运工
	pich      chan Piece     // 分片配置获取渠道
	dtch      chan PieceData // 数据传递渠道
}

//
// NewDownloader 新建一个下载器。
//
func NewDownloader(fx func() Hauler) *Downloader {
	return &Downloader{NewHauler: fx}
}

//
// Run 执行下载。
// rest 为外部传递的一个待下载分片下标集。
// 返回一个分片数据读取通道。
// 当下载进程完毕后，通道关闭（可能有下载失败）。
//
func (d *Downloader) Run(span int, rest []int64) <-chan PieceData {
	if len(rest) == 0 {
		return nil
	}
	// 搬运工数量
	max := MaxThread
	if len(rest) < max {
		max = len(rest)
	}
	// max作为通道缓存仅是一种主观处理。
	// 通道的效率与外部存储IO相关。
	d.dtch = make(chan PieceData, max)

	// 分片索引服务
	d.pich = pieceGetter(rest, int64(span))

	err := goes.WorksLong(goes.LimitTasker(d, max), nil)
	go func() {
		for _ = range err {
			// 忽略下载失败
		}
		close(d.dtch)
	}()

	return d.dtch
}

///////////////////
// Tasker 接口实现
///////////////////////////////////////////////////////////////////////////////

//
// Task 获取一个分片定义。
//
func (d *Downloader) Task() (k interface{}, ok bool) {
	k, ok = <-d.pich
	return
}

//
// Work 下载单块数据。
// 下载失败时无数据传递。
//
func (d *Downloader) Work(k interface{}) error {
	p := k.(Piece)
	bs, err := d.NewWork().Get(p)

	if err != nil {
		return err
	}
	d.dtch <- PieceData{p.Begin, bs}
	return nil
}

//
// 分片定义取值渠道。
// 对外传递未下载分片定义{Begin, End}。
//
func pieceGetter(list []int64, span int64) <-chan Piece {
	ch := make(chan Piece)

	go func() {
		for _, off := range list {
			ch <- Piece{off, off + span}
		}
		close(ch)
	}()

	return ch
}
