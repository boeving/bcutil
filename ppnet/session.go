package ppnet

///////////////
/// 会话与校验。
/// 用于两个端点的当前连系认证和数据校验。
/// 应用初始申请一个会话时，对端发送一个8字节随机值作为验证前缀。
/// 该验证前缀由双方保存，不再在网络上传输。
/// 之后的数据传输用一个固定的方式计算CRC32校验和。
/// 算法：
///  CRC32(
///  	会话校验码 +
///  	数据ID #SND + 数据ID #RCV +
///  	序列号 + 确认号 +
///  	确认距离 + 发送距离 +
///  	数据
///  )
/// 每一次的该值都会不一样，它既是对数据的校验，也是会话安全的认证。
/// 注：
/// 这仅提供了简单的安全保护，主要用于防范基于网络性能的攻击。
/// 对于重要的数据，应用应当自行设计强化的安全措施。
///////////////////////////////////////////////////////////////////////////////

type session struct {
	//
}
