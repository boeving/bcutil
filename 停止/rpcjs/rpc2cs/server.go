package main

import (
	"fmt"
	"log"
	"net"

	"github.com/qchen-zh/pputil/停止/rpcjs"
	"github.com/qchen-zh/pputil/停止/rpcjs/rpc2cs/server"
	"github.com/tinylib/msgp/msgp"
)

func main() {
	// arith := new(server.Arith)
	// serv := rpcjs.NewServer()
	// serv.Register(arith)

	l, e := net.Listen("tcp", ":1234")
	if e != nil {
		log.Fatal("listen error:", e)
	}
	fmt.Println("start listen for client...")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Print("rpcjs.Serve: accept:", err.Error())
			return
		}
		// go serv.ServeConn(conn)
		var req rpcjs.Request
		var arg server.Args
		var arg2 server.Args
		rd := msgp.NewReader(conn)
		// msgp.Decode(conn, &req)
		req.DecodeMsg(rd)
		fmt.Println(req)

		// msgp.Decode(conn, &arg)
		arg.DecodeMsg(rd)

		fmt.Println(arg)

		// msgp.Decode(conn, &arg2)
		arg2.DecodeMsg(rd)
		fmt.Println(arg2)
	}
}

func aclient(conn net.Conn) {
	fmt.Println("a client")
	client := rpcjs.NewClient(conn)

	args := &server.Args{30, 8}

	var reply server.Quotient

	fmt.Println("client call...")
	err := client.Call("Arith.Divide", args, &reply)
	if err != nil {
		log.Fatal("arith error:", err)
	}
	fmt.Println("client call end...")

	fmt.Printf("Arith: %d, %d => %v\n", args.A, args.B, reply)

	args = &server.Args{130, 8}
	fmt.Println("client call...")
	err = client.Call("Arith.Divide", args, &reply)
	if err != nil {
		log.Fatal("arith error:", err)
	}
	fmt.Println("client call end...")

	fmt.Printf("Arith: %d, %d => %v\n", args.A, args.B, reply)

}
