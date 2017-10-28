package main

import (
	"fmt"
	"log"
	"net"

	"github.com/qchen-zh/pputil/废弃/rpcjs"
	"github.com/qchen-zh/pputil/废弃/rpcjs/rpc2cs/server"
	"github.com/tinylib/msgp/msgp"
)

const serverAddress = "127.0.0.1"

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:1234")
	if err != nil {
		log.Fatal("dialing:", err)
	}
	// go aserver(conn)
	// client := rpcjs.NewClient(conn)

	ids := rpcjs.Request{1, "Arith.Divide"}
	args := server.Args{int('z'), int('a')}
	args2 := server.Args{int('x'), int('i')}

	// var reply server.Quotient
	// err = client.Call("Arith.Divide", args, &reply)

	msgp.Encode(conn, ids)
	// 添加完全无关的该行后接收端正常，
	// 且接收端接收的args/args2值不确定。
	// 此为已脱离rpc方式的测试。
	// log.Println("hai")
	msgp.Encode(conn, args)
	// log.Println("hai2") // 同上效果
	msgp.Encode(conn, args2)
	if err != nil {
		log.Fatal("arith error:", err)
	}
	// fmt.Printf("Arith: %d, %d => %v\n", args.A, args.B, reply)

	// time.Sleep(10 * time.Second)
	// fmt.Println("done!")
}

func aserver(conn net.Conn) {
	arith := new(server.Arith)

	serv := rpcjs.NewServer()
	serv.Register(arith)

	fmt.Println("server start...")
	serv.ServeConn(conn)
	fmt.Println("server end...")
}
