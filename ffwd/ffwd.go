package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s <localPort> <remoteHost> <remotePort>\n", os.Args[0])
		os.Exit(1)
	}
	localPort := os.Args[1]
	remoteHost := os.Args[2]
	remotePort := os.Args[3]
	newConns := make(chan net.Conn)

	go tcpListen(localPort, newConns)
	for conn := range newConns {
		go tcpRedirect(conn, remoteHost, remotePort)
	}
}

func tcpListen(port string, newConns chan<- net.Conn) {
	// start listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	fatalIfErr("Listen", err)
	fmt.Printf("[*] Listening on port %s\n", port)

	for {
		// accept and handle connection
		conn, err := listener.Accept()
		if err != nil {
			logIfErr("Accept", err)
		} else {
			fmt.Printf("[*] Connected to %s\n\n", conn.RemoteAddr().String())
			newConns <- conn
		}
	}
}

func tcpRedirect(srcConn net.Conn, target, port string) {
	defer srcConn.Close()
	
	// if connection fails, bubble up error
	targetConn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", target, port))
	if err != nil {
		logErr("Dial", err)
		fmt.Fprintf(srcConn, "Somewhere: %s\n", err)
		return
	}
	defer targetConn.Close()

	// rewire I/O
	done := make(chan struct{})
	go func(){ io.Copy(srcConn, targetConn); done<-struct{}{}; }()
	go func(){ io.Copy(targetConn, srcConn); done<-struct{}{}; }()
	<-done
}

func fatalIfErr(context string, err error) {
	if err != nil {
		log.Fatalf("[!] %s:\n%s\n", context, err)
	}
}

func logIfErr(context string, err error) {
	if err != nil {
		logErr(context, err)
	}
}

func logErr(context string, err error) {
	log.Printf("[!] %s -> %s\n", context, err)
}
