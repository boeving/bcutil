package dcp

//
// Fd 文件描述符。
// 这仅是一种类似，用于标识对一个数据体的操作。
//
type Fd int

//
// Client 客户端接口。
//
type Client interface {
	Receiver

	// 此接口由外部用户调用。
	Request(res []byte) int

	// 中断连系。
	// 结束数据接收。如果有未完成的数据接收，返回一个错误。
	Break(id int) error
}

//
// Receiver 接收器接口。
// 它由发出请求的客户端应用实现。获取数据并处理。
// 由内部的DCP服务进程调用。
//
type Receiver interface {
	// 新建一个操作。
	// 传入请求对应的数据ID，返回一个操作标识符。
	Open(id int) (Fd, error)

	// 处理传入的数据。
	// 同时会传入New()返回的操作标识符。
	Process(Fd, []byte) error

	// 处理完毕调用。
	// 实现可能需要清理/释放某些资源。
	Close(Fd) error
}

//
// 一个客户端实现。
//
type client struct {
	//
}

func (c *client) Request(res []byte) int {
	//
}

func Dial(network string, laddr, raddr *DCPAddr) (*Thread, error) {
	//
}
