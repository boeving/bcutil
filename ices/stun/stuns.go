package stuns

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

func sendMessage(msg *Message, conn *net.UDPAddr, udp_conn *net.UDPConn) {
	buffer, err := json.Marshal(msg)
	if err != nil {
		log.Println("can't marshal message: ", err)
		return
	}

	_, err = udp_conn.WriteToUDP(buffer, conn)
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

func handleMessage(raw_data []byte, conn *net.UDPAddr, udp_conn *net.UDPConn) {
	var msg Message

	err := json.Unmarshal(raw_data, &msg)
	if err != nil {
		log.Println("can't parse message: ", err)
		sendMessage(&Message{msg.SeqNum, ERROR_CMD, "bad message format"},
			conn, udp_conn)
		return
	}

	if !msgIsGood(&msg) {
		sendMessage(&Message{msg.SeqNum, ERROR_CMD, "bad message body"},
			conn, udp_conn)
		return
	}

	mutex.Lock()
	defer mutex.Unlock()

	switch msg.Cmd {
	case UPDATE_ADDR_CMD:
		mobile_to_addr[msg.Body] = *conn
		sendMessage(&Message{msg.SeqNum, UPDATE_ADDR_CMD, ""}, conn, udp_conn)

	case GET_ADDR_CMD:
		if address, ok := mobile_to_addr[msg.Body]; ok {
			sendMessage(&Message{msg.SeqNum, GET_ADDR_CMD, address.String()},
				conn, udp_conn)
		} else {
			log.Println("can't find addr for " + msg.Body)
			sendMessage(&Message{msg.SeqNum, ERROR_CMD, "addr not found"},
				conn, udp_conn)
		}

	default:
		log.Println("unknown command")
	}
}

func main() {
	udp_addr, err := net.ResolveUDPAddr("udp4", ":65000")
	if err != nil {
		log.Println("can't resolve udp: ", err)
		os.Exit(1)
	}

	udp_conn, err := net.ListenUDP("udp4", udp_addr)
	if err != nil {
		log.Println("can't listen udp: ", err)
		os.Exit(1)
	}
	defer udp_conn.Close()

	buffer := make([]byte, BUFFER_SIZE)
	for {
		n, conn, err := udp_conn.ReadFromUDP(buffer)
		if err != nil {
			log.Println("can't read from udp: ", err)
			continue
		}

		go handleMessage(buffer[0:n], conn, udp_conn)
	}
}
