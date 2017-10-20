package peerjs

//
// Request 客户端向服务器的请求条目。
// 方法参数可自由定制，与服务器端的参数要求对应（msgp.Decodable）即可。
// ID强制要求为一个数字以简化逻辑。
// 注：客户端应用msgp.Encode生成可匹配的输出。
//
type Request struct {
	ID     uint64 `msg:"id"`     // 请求序列号，强制为数字
	Method string `msg:"method"` // 格式： "Service.Method"
}

//
// Response 服务器对客户端发送的响应头。
// 注：具体的响应数据在其自身文件中定义，需支持 msgp.Encodable 接口。
//
type Response struct {
	ID    uint64 `msg:"id"`    // 对应请求的序列号
	Error string `msg:"error"` // 非空会导致无响应数据且关闭连接
}
