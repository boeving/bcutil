package download

import (
	"crypto/sha256"
	"errors"

	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/goes"
)

const (
	// MaxThread 默认下载协程数
	MaxThread = 8
)

var errChkSum = errors.New("checksum not exist or not match")

//
// Getter 数据获取器。
// 实施单个目标（分片）的具体下载行为，
//
type Getter interface {
	// 获取数据。
	Get(piece.Piece) ([]byte, error)
}

//
// Hauler 数据搬运工。
// 新建一个数据获取器接口。
//
type Hauler interface {
	Getter() Getter
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
// 分片下载，校验。向外发送合格的分片数据。
// 若不赋值验证集，则不执行验证（如http直接下载）。
//
type Downloader struct {
	Haul Hauler                  // 数据搬运工
	Sums map[int64]piece.HashSum // 验证集（可选）
	pich <-chan piece.Piece      // 分片配置获取渠道
	dtch chan PieceData          // 数据传递渠道
}

//
// Run 执行下载。
// rest 为外部传递的一个待下载分片下标集。
// 返回一个分片数据读取通道。
// 当下载进程完毕后，通道关闭（可能有下载失败）。
//
func (d *Downloader) Run(span int64, rest []int64) <-chan PieceData {
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
	d.pich = pieceGetter(rest, span)

	err := goes.WorksLong(goes.LimitTasker(d, max))
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
// 下载失败或校验不符合时无数据传递。
//
func (d *Downloader) Work(k interface{}) error {
	p := k.(piece.Piece)
	bs, err := d.Haul.Getter().Get(p)

	if err != nil {
		return piece.Error{Off: p.Begin, Err: err}
	}
	if d.Sums != nil {
		sum, ok := d.Sums[p.Begin]
		// 不合格丢弃
		if !ok || sum != sha256.Sum256(bs) {
			return piece.Error{Off: p.Begin, Err: errChkSum}
		}
	}
	d.dtch <- PieceData{p.Begin, bs}

	return nil
}

//
// 分片定义取值渠道。
// 对外传递未下载分片定义{Begin, End}。
//
func pieceGetter(list []int64, span int64) <-chan piece.Piece {
	ch := make(chan piece.Piece)

	go func() {
		for _, off := range list {
			ch <- piece.Piece{Begin: off, End: off + span}
		}
		close(ch)
	}()

	return ch
}
