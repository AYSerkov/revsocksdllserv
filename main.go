package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"

	"strconv"
	"time"

	"github.com/hashicorp/yamux"
)

var agentpassword string
var socksdebug bool

type Client struct {
	Password string
}

var proxytout = time.Millisecond * 1000 //timeout for wait magicbytes

// listen for agents
func listenForAgents(tlslisten bool, address string, clients string, certificate string) error {
	var err, erry error
	var cer tls.Certificate
	var session *yamux.Session
	var sessions []*yamux.Session
	var ln net.Listener

	// var yconfig *yamux.Config
	// var YamuxConfig *yamux.Config

	// yconfig.EnableKeepAlive = true
	// yconfig.KeepAliveInterval = 5 * time.Second

	log.Printf("Will start listening for clients on %s", clients)
	if tlslisten {
		log.Printf("Listening for agents on %s using TLS", address)
		if certificate == "" {
			cer, err = getRandomTLS(2048)
			log.Println("No TLS certificate. Generated random one.")
		} else {
			cer, err = tls.LoadX509KeyPair(certificate+".crt", certificate+".key")
		}
		if err != nil {
			log.Println(err)
			return err
		}
		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		ln, err = tls.Listen("tcp", address, config)
	} else {
		log.Printf("Listening for agents on %s", address)
		ln, err = net.Listen("tcp", address)
	}
	if err != nil {
		log.Printf("Error listening on %s: %v", address, err)
		return err
	}
	var listenstr = strings.Split(clients, ":")
	portnum, errc := strconv.Atoi(listenstr[1])
	if errc != nil {
		log.Printf("Error converting listen str %s: %v", clients, errc)
	}
	portinc := 0
	for {
		conn, err := ln.Accept()
		conn.RemoteAddr()
		agentstr := conn.RemoteAddr().String()
		log.Printf("[%s] Got a connection from %v: ", agentstr, conn.RemoteAddr())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Errors accepting!")
		}

		reader := bufio.NewReader(conn)

		//read only 64 bytes with timeout=1-3 sec. So we haven't delay with browsers
		// conn.SetReadDeadline(time.Now().Add(proxytout))
		// statusb := make([]byte, 64)
		// _, _ = io.ReadFull(reader, statusb)

		//Alternatively  - read all bytes with timeout=1-3 sec. So we have delay with browsers, but get all GET request
		conn.SetReadDeadline(time.Now().Add(proxytout))
		statusb, _ := ioutil.ReadAll(reader)

		var recClientInfo Client
		err = json.Unmarshal(statusb, &recClientInfo)
		if err != nil {
			log.Println(err)
		}
		// log.Printf("magic bytes: %v", statusb[:64])
		// log.Printf("magic bytes: %v", statusb)

		// if hex.EncodeToString(statusb) != agentpassword {
		// if string(statusb[:64]) != agentpassword {
		// if string(statusb)[:len(agentpassword)] != agentpassword {
		if recClientInfo.Password != agentpassword {
			//do HTTP checks
			log.Printf("Received request: %v", string(statusb[:64]))
			status := string(statusb)
			if strings.Contains(status, " HTTP/1.1") {
				httpresonse := "HTTP/1.1 301 Moved Permanently" +
					"\r\nContent-Type: text/html; charset=UTF-8" +
					"\r\nLocation: https://www.youtube.com/watch?v=oHg5SJYRHA0" +
					"\r\nServer: Apache" +
					"\r\nContent-Length: 0" +
					"\r\nConnection: close" +
					"\r\n\r\n"

				conn.Write([]byte(httpresonse))
				conn.Close()
			} else {
				conn.Close()
			}

		} else {
			//magic bytes received.
			//disable socket read timeouts
			// log.Printf("[%s] Got Client. Hostname: %s. IPs: %v", agentstr, ClientInfo.Hostname, ClientInfo.IPAddr)
			log.Printf("[%s] Got Client from %s", agentstr, conn.RemoteAddr())
			conn.SetReadDeadline(time.Now().Add(600 * time.Hour))
			// conn.SetReadDeadline(time.Now().Add(100 * time.Hour))
			// conn.SetReadDeadline(time.Now().Add(5 * time.Hour))

			// yamuxconfig := yamux.DefaultConfig()
			// yamuxconfig.KeepAliveInterval = 5 * time.Second
			// yamuxconfig.StreamCloseTimeout = 1 * time.Minute
			// yamuxconfig.EnableKeepAlive = true
			// yamuxconfig.LogOutput = os.Stdout
			// fmt.Println("yamuxconfig:", yamuxconfig.EnableKeepAlive)

			//Add connection to yamux
			session, erry = yamux.Client(conn, nil)
			// session, erry = yamux.Client(conn, yamuxconfig)
			if erry != nil {
				log.Printf("[%s] Error creating client in yamux for %s: %v", agentstr, conn.RemoteAddr(), erry)
				continue
			}
			sessions = append(sessions, session)
			go listenForClients(agentstr, listenstr[0], portnum+portinc, session, recClientInfo)
			portinc = portinc + 1
		}
	}
	// return nil
}

// Catches local clients and connects to yamux
func listenForClients(agentstr string, listen string, port int, session *yamux.Session, recClientInfo Client) error {
	var ln net.Listener
	var address string
	var err error
	portinc := port
	for {
		address = fmt.Sprintf("%s:%d", listen, portinc)
		//address = fmt.Sprintf("%s:%d", "185.240.103.195", portinc)
		fmt.Printf("[%s] Waiting for clients on %s", agentstr, address)
		//fmt.Printf("[%s] Waiting for clients on %s. SOCKS from Hostname: %s, IPs %v", agentstr, address, recClientInfo.Hostname, recClientInfo.IPAddr)
		// fmt.Printf("[%s] | %s | %s | %s |\n", agentstr, recClientInfo.Hostname, recClientInfo.IPAddr, address)
		//fmt.Printf("%s | %s | %s |\n", recClientInfo.Hostname, recClientInfo.IPAddr, address)
		ln, err = net.Listen("tcp", address)
		if err != nil {
			fmt.Printf("[%s] Error listening on %s: %v", agentstr, address, err)
			portinc = portinc + 1
		} else {
			break
		}
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[%s] Error accepting on %s: %v", agentstr, address, err)
			return err
		}
		if session == nil {
			log.Printf("[%s] Session on %s is nil", agentstr, address)
			conn.Close()
			continue
		}
		log.Printf("[%s] Got client. Opening stream for %s", agentstr, conn.RemoteAddr())

		stream, err := session.Open()
		if err != nil {
			log.Printf("[%s] Error opening stream for %s: %v", agentstr, conn.RemoteAddr(), err)
			return err
		}

		// connect both of conn and stream

		go func() {
			log.Printf("[%s] Starting to copy conn to stream for %s", agentstr, conn.RemoteAddr())
			io.Copy(conn, stream)
			conn.Close()
			log.Printf("[%s] Done copying conn to stream for %s", agentstr, conn.RemoteAddr())
		}()
		go func() {
			log.Printf("[%s] Starting to copy stream to conn for %s", agentstr, conn.RemoteAddr())
			io.Copy(stream, conn)
			stream.Close()
			log.Printf("[%s] Done copying stream to conn for %s", agentstr, conn.RemoteAddr())
		}()
	}
}

func main() {

	listen := flag.String("listen", "", "listen port for receiver address:port")
	certificate := flag.String("cert", "", "certificate file")
	socks := flag.String("socks", "127.0.0.1:1080", "socks address:port")
	// connect := flag.String("connect", "", "connect address:port")
	optproxytimeout := flag.String("proxytimeout", "", "proxy response timeout (ms)")
	optpassword := flag.String("pass", "", "Connect password")
	// recn := flag.Int("recn", 3, "reconnection limit")
	optverbose := flag.Bool("v", false, "verbose")

	// rect := flag.Int("rect", 30, "reconnection delay")
	fsocksdebug := flag.Bool("debug", false, "display debug info")
	// version := flag.Bool("version", false, "version information")
	// flag.Usage = func() {

	// 	fmt.Println("revsocks - reverse socks5 server/client")
	// 	fmt.Println("")
	// 	flag.PrintDefaults()
	// 	fmt.Println("")
	// 	fmt.Println("Usage:")
	// 	fmt.Println("1) Start on the client: revsocks -listen :8080 -socks 127.0.0.1:1080 -pass test")
	// 	fmt.Println("2) Start on the server: revsocks -connect client:8080 -pass test")
	// 	fmt.Println("3) Connect to 127.0.0.1:1080 on the client with any socks5 client.")
	// }

	flag.Parse()
	if !*optverbose {
		log.SetOutput(ioutil.Discard)
	}

	if *fsocksdebug {
		socksdebug = true
	}
	// if *version {
	// 	fmt.Println("revsocks - reverse socks5 server/client")
	// 	os.Exit(0)
	// }

	if *listen != "" {
		log.Println("Starting to listen for clients")
		if *optproxytimeout != "" {
			opttimeout, _ := strconv.Atoi(*optproxytimeout)
			proxytout = time.Millisecond * time.Duration(opttimeout)
		} else {
			proxytout = time.Millisecond * 1000
		}

		if *optpassword != "" {
			agentpassword = *optpassword
		} else {
			agentpassword = RandString(64)
			log.Println("No password specified. Generated password is " + agentpassword)
		}

		//listenForSocks(*listen, *certificate)
		log.Fatal(listenForAgents(true, *listen, *socks, *certificate))
	}

	// flag.Usage()
	fmt.Fprintf(os.Stderr, "You must specify a listen port or a connect address\n")
	os.Exit(1)
}
