package peepb

/////////////////////
// 端对端数据分片下载
// 需管理连接端，均衡询问和请求。
// 1. 分片询问。小批量分片数据询问，返回有效子集（分片）。
// 2. 分片数据请求。在1的基础上获取分片数据。
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"

	"github.com/qchen-zh/pputil/download"
	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/ppconn"
	context "golang.org/x/net/context"
)

// 数据传输错误。
// 命名错误，可用于目标端点的评估。
var (
	ErrPiece     = errors.New("the piece(end point) is invalid")
	ErrEmpty     = errors.New("no data on server")
	ErrPieceData = errors.New("the piece data is invalid")
)

//
// Size 分片大小。
// config.pb.go 补充定义，与 piece.Piece 定义相同。
//
func (p *Piece) Size() int {
	return int(p.End - p.Begin)
}

//
// HashDl 通过哈希标识下载。
// 对 Hauler 接口的实现，管理不同连接端均衡分派请求。
//
type HashDl struct {
	pool []*hashGetter
}

//
// NewHashDl 新建一个哈希下载管理器。
//
// ps 为待下载的分片集，用于连接端预先询问，合理组织下载请求。
// pool 为端点连接池的外部引用，这里用于联系使用和评分。
// （注：连接池自身完成端点连接的更新逻辑）
//
func NewHashDl(ps []piece.Piece, pool *ppconn.Pool) *HashDl {
	//
}

//
// NewGetter 新建一个数据搬运工。
// 实现 download.Hauler 接口。返回自身即可，并发安全。
//
func (h *HashDl) NewGetter() download.Getter {
	return h
}

//
// Get 获取分片数据。
// 根据分片索引返回拥有该分片数据的获取器（hashGetter，封装了连接）。
// 对 download.Getter 接口的实现，但为管理者角色。
//
func (h *HashDl) Get(p piece.Piece) ([]byte, error) {
	//
}

//
// 分片数据搬运工。
// 对 Getter 接口的实现。对应一个的RPC的服务对端。
// DC 由 NewDLClient() 创建，需要一个grpc连接（grpc.Dial()）。
//
type hashGetter struct {
	DC DLClient
}

//
// Get 下载当前分片。
// 实现 download.Getter 接口。
// 如果p.End不大于零则无效（P2P方式不传递文件整体）。
//
// 返回不同类型的错误，可用于评估目标端点价值。
//
func (h *hashGetter) Get(p piece.Piece) ([]byte, error) {
	if p.End <= 0 {
		return nil, ErrPiece
	}
	pp := Piece(p)
	// RPC Call
	buf, err := h.DC.Get(context.Background(), &pp)
	if err != nil {
		return nil, err // ErrEmpty
	}
	if buf.Offset != p.Begin || len(buf.Bytes) != p.Size() {
		return nil, ErrPieceData
	}
	return buf.Bytes, err
}

//
// DlServe P2P下载服务端。
// 实现 DLServer 接口，支持其它端点对分片数据的下载请求（RPC）。
// Cache 对应下载目标的一个既有缓存，数据源，不可为空。
//
// 不支持直接从外部存储获取分片数据，它在缓存器一级实现。
//
type DlServe struct {
	Cache *download.Cache
}

//
// Get 获取下载分片数据。
// 支持相同起点偏移的小分片传递（相同文件但分片规划并不相同时）。
//
func (ds DlServe) Get(ctx context.Context, args *Piece) (*PieceData, error) {
	pd := ds.Cache.Get(args.Begin)

	if pd == nil ||
		pd.Offset != args.Begin || len(pd.Bytes) < args.Size() {
		return nil, ErrEmpty
	}
	return &PieceData{
		pd.Offset,
		pd.Bytes[:args.Size()],
	}, nil
}
