P2P文件系统
模拟文件的读取/写入接口，实现对对端的数据请求和发送。
打开逻辑是一个查询（Query），即数据询问，成功回调传递一个2字节整数（操作句柄/数据ID）。
伪代码：
	interface Handler {
		Done(int)
		Fail(error)
	}
	function Query(URL/URI/Hash, Handler)

依赖dcp包，数据控制协议。
