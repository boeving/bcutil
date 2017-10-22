package peerd

//
// Piece 分片（RPC请求参数）。
// 结构与piece.Piece相同但完整定义，便于msgp单独编码。
//
type Piece struct {
	Begin int64
	End   int64
}

//
// PieceData 分片数据
// （RPC响应数据）。
//
type PieceData struct {
	Offset int64
	Bytes  []byte
}
