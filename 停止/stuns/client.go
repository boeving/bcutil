package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
	"bytes"
)

func main() {
	raddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:17788")
	log.Println("Remote address: ", raddr, err)

	laddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:7788")
	log.Println("Local: ", laddr, err)

	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		log.Fatalln(err)
	}

	time.Sleep(3*time.Second)

	fmt.Println(conn.Write([]byte("The First line..")))
	fmt.Println(conn.Write([]byte("The Second line..")))
	fmt.Println(conn.Write([]byte("The Three line..")))

	 dd := bytes.Repeat([]byte("Hello"), 100)
	 fmt.Println(conn.Write(dd))

	time.Sleep(3*time.Second)
	conn.Close()

	//go mustCopy(os.Stdout, conn)
	fmt.Println("UDP Send Done")
}

func mustCopy(dst io.Writer, src io.Reader) {
	if _, err := io.Copy(dst, src); err != nil {
		log.Fatal(err)
	}
}
