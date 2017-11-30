package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

func main() {
	raddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:7788")
	log.Println("Server address: ", raddr, err)

	laddr, err := net.ResolveUDPAddr("udp", ":27788")
	log.Println("Local: ", laddr, err)

	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		log.Fatalln(err)
	}
	defer conn.Close()
	go mustCopy(os.Stdout, conn)

	mustCopy(conn, os.Stdin)
	fmt.Println("UDP Dial End")
}

func mustCopy(dst io.Writer, src io.Reader) {
	if _, err := io.Copy(dst, src); err != nil {
		log.Fatal(err)
	}
}
