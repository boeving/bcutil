package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	//"os"
)

func main() {
	saddr, err := net.ResolveUDPAddr("udp", ":7788")
	if err != nil {
		log.Fatalln(err)
	}
/*
	go func() {
		//laddr, _ := net.ResolveUDPAddr("udp", ":7799")
		//raddr, _ := net.ResolveUDPAddr("udp", "192.168.31.154:17799")
		raddr, _ := net.ResolveUDPAddr("udp", "192.168.31.24:17799")
		conn, err := net.DialUDP("udp", saddr, raddr)
		if err != nil {
			log.Fatalln(err)
		}
		defer conn.Close()

		//go mustCopy(os.Stdout, conn)
		//mustCopy(conn, os.Stdin)
		var buf [10]byte
		fmt.Println(conn.ReadFromUDP(buf[:]))
	}()
*/
	listen(saddr)

	fmt.Println("UDP Server Done!")
}

func listen(laddr *net.UDPAddr) {
	log.Println("Listen: ", laddr)

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln(err)
	}

	raddr, _ := net.ResolveUDPAddr("udp", "192.168.31.24:17799")
	if _, err := conn.WriteTo([]byte("Hai, I'm in Listening...\n\n"), raddr); err != nil {
		log.Println(err)
	}

	buf := make([]byte, 128)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil || n == 0 {
			fmt.Println(err)
			break
		}
		fmt.Println(strings.Trim(string(buf), "\000"), n, addr, err)
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
