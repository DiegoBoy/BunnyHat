package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

func main() {
	target := "scanme.nmap.org"
	work := make(chan int, 512)
	results := make(chan int)
	var openPorts []int
	dialer := &net.Dialer{ Timeout: time.Second * 5 }
	start := time.Now()

	// start workers
	for i := 1; i <= cap(work); i++ {
		go tcpConnectWorker(dialer, target, work, results)
	}

	// send work
	go func(){ 
		for i := 1; i <= 65535; i++ { 
			work <- i 
		}
	}()

	// wait for results
	for i := 1; i <= 65535; i++ {
		if port := <-results; port != 0 {
			fmt.Print("Open: ")
			fmt.Println(port)
			openPorts = append(openPorts, port)
		}
	}
	close(work)
	close(results)

	fmt.Println("# Results:")
	sort.Ints(openPorts)
	for _, port := range openPorts {
		fmt.Println(port)
	}
	end := time.Now()
	fmt.Printf("Scanned in %.2fs\n", end.Sub(start).Seconds())
}

func tcpConnectWorker(dialer *net.Dialer, target string, in, out chan int) {
	for port := range in {
		if ScanPortTcpConnect(dialer, target, port) {
			out <- port
		} else {
			out <- 0
		}
	}
}

func ScanPortTcpConnect(dialer *net.Dialer, target string, port int) bool {
	address := fmt.Sprintf("%s:%d", target, port)
	if conn, err := dialer.Dial("tcp", address); err == nil { // port open
		conn.Close()
		return true
	} else {
		if !strings.HasSuffix(err.Error(), "connect: connection refused") && // port closed
		   !strings.HasSuffix(err.Error(), "i/o timeout") { // port filtered
			fmt.Println(err)
		}
		return false
	}
}
