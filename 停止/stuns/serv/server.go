package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

func main() {
	saddr, err := net.ResolveUDPAddr("udp", ":7788")
	if err != nil {
		log.Fatalln(err)
	}

	go func() {
		laddr, _ := net.ResolveUDPAddr("udp", ":7799")
		raddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:17799")
		conn, err := net.DialUDP("udp", laddr, raddr)
		if err != nil {
			log.Fatalln(err)
		}
		defer conn.Close()

		go mustCopy(os.Stdout, conn)
		mustCopy(conn, os.Stdin)
	}()
	listen(saddr)

	fmt.Println("UDP Server Done!")
}

func listen(laddr *net.UDPAddr) {
	log.Println("Listen: ", laddr)

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln(err)
	}

	buf := make([]byte, 512)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil || n == 0 {
			fmt.Println(err)
			break
		}
		fmt.Println(string(buf), n, addr, err)
	}
	conn.Close()
}

func mustCopy(dst io.Writer, src io.Reader) error {
	// fmt.Fprintln(os.Stdout, src.RemoteAddr())

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}
