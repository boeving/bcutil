package ppconn

///////////////
// 端点连接池
// 管理连接池中的端点组合，提供端点连接和端点评分方法。
// 分值过低或连接期失效，更新连接端。
//
// 两级更新定义：
// 1. 全更新计数（<64k）。
// 2. 连接池ID当前周期唯一（ 64k回绕）。
// 注：
// - 每端点连接保持通常为1-2小时（连接期）。
// - 64k回绕长度保证最大2字节数据长度。
///////////////////////////////////////////////////////////////////////////////

const (
	connMaxCounts = 0xffff // 最大连接计数
)

//
// Pool 端点连接池。
//
type Pool struct {
	cntID, cntAll int
}
