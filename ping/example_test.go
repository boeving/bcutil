package ping_test

import (
	"errors"
	"log"
	"net"
	"os"
	"time"

	"github.com/qchen-zh/pputil/ping"
	"golang.org/x/net/icmp"
)

// Reverb 简单回响
type Reverb struct{}

//
// Receive 成功接收。
func (r Reverb) Receive(a net.Addr, id int, echo *icmp.Echo, exit func()) error {
	if id != echo.ID {
		return errors.New("id not match message's ID")
	}
	rtt := time.Since(ping.BytesToTime(echo.Data[:ping.TimeSliceLength]))
	log.Println(a, rtt)
	return nil
}

// Fail 失败显示。
func (r Reverb) Fail(a net.Addr, err error, exit func()) {
	log.Println(a, err)
}

var network = "ip"

// 单次ping。
func Example_Ping() {
	ip, err := net.ResolveIPAddr(network, os.Args[1])
	if err != nil {
		log.Fatalln(err)
	}
	conn, err := ping.Listen(network, "0.0.0.0")
	if err != nil {
		log.Fatalln(err)
	}

	var proc Reverb
	p, _ := ping.NewPinger(network, conn, proc)

	// 启动接收监听服务
	p.Serve()

	// 最多3次失败再尝试
	p.Ping(ip, 3)

	time.Sleep(time.Second * 3)
	p.Exit()
}
