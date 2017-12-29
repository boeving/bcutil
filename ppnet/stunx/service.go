package stunx

///////////////
// STUN 服务端
// 仅支持单个IP地址，需与其它STUN服务端协作检测客户NAT类型。
///////////////////////////////////////////////////////////////////////////////

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
)

// UDP receive buffer size
var BUFFER_SIZE = 1024

var mutex = &sync.Mutex{}
var mobile_to_addr = make(map[string]net.UDPAddr)

type Message struct {
	SeqNum int    `json:"seq_num"`
	Cmd    int    `json:"cmd"`
	Body   string `json:"body"`
}

// Available command types
const (
	ERROR_CMD = iota
	UPDATE_ADDR_CMD
	GET_ADDR_CMD
)

func sendMessage(msg *Message, addr *net.UDPAddr, conn *net.UDPConn) {
	buffer, err := json.Marshal(msg)
	if err != nil {
		log.Println("can't marshal message: ", err)
		return
	}

	_, err = conn.WriteToUDP(buffer, addr)
	if err != nil {
		log.Println("can't send message: ", err)
		return
	}
}

func msgIsGood(msg *Message) bool {
	if _, err := strconv.Atoi(msg.Body); err != nil {
		log.Println("can't parse phone number: ", err)
		return false
	}
	return true
}

func handleMessage(raw_data []byte, addr *net.UDPAddr, conn *net.UDPConn) {
	var msg Message

	err := json.Unmarshal(raw_data, &msg)
	if err != nil {
		log.Println("can't parse message: ", err)
		sendMessage(&Message{msg.SeqNum, ERROR_CMD, "bad message format"},
			addr, conn)
		return
	}

	if !msgIsGood(&msg) {
		sendMessage(&Message{msg.SeqNum, ERROR_CMD, "bad message body"},
			addr, conn)
		return
	}

	mutex.Lock()
	defer mutex.Unlock()

	switch msg.Cmd {
	case UPDATE_ADDR_CMD:
		mobile_to_addr[msg.Body] = *addr
		sendMessage(&Message{msg.SeqNum, UPDATE_ADDR_CMD, ""}, addr, conn)

	case GET_ADDR_CMD:
		if address, ok := mobile_to_addr[msg.Body]; ok {
			sendMessage(&Message{msg.SeqNum, GET_ADDR_CMD, address.String()},
				addr, conn)
		} else {
			log.Println("can't find addr for " + msg.Body)
			sendMessage(&Message{msg.SeqNum, ERROR_CMD, "addr not found"},
				addr, conn)
		}

	default:
		log.Println("unknown command")
	}
}

func main() {
	addr, err := net.ResolveUDPAddr("udp4", ":65000")
	if err != nil {
		log.Println("can't resolve udp: ", err)
		os.Exit(1)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		log.Println("can't listen udp: ", err)
		os.Exit(1)
	}
	defer conn.Close()

	buf := make([]byte, BUFFER_SIZE)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("can't read from udp: ", err)
			continue
		}

		go handleMessage(buf[0:n], addr, conn)
	}
}
