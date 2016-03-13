// A small SSH daemon providing bash sessions
//
// Server:
// cd my/new/dir/
// #generate server keypair
// ssh-keygen -t rsa
// go get -v .
// go run sshd.go
//
// Client:
// ssh foo@localhost -p 2200 #pass=bar

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"net"

	"github.com/nu7hatch/gouuid"

	"golang.org/x/crypto/ssh"
)

type clientHandler func(client sshClient)
type ScreenSize struct {
	width, height uint32
}
type sshClient struct {
	channel ssh.Channel
	resizes chan ScreenSize
	id      *uuid.UUID
}

func (c *sshClient) Disconnect() {
	log.Printf("Client %s hanging up", c.id)
	c.channel.Write([]byte("bye\n\r"))
	c.channel.Close()
}

func main() {
	newClientHandler := func(client sshClient) {
		message := fmt.Sprintf("welcome, id: %s\n\r", client.id)
		client.channel.Write([]byte(message))

		// handle resizes
		go func() {
			for size := range client.resizes {
				message := fmt.Sprintf("resize to %d x %d\n\r", size.width, size.height)
				client.channel.Write([]byte(message))
			}
		}()

		// handle incoming bytes
		go func() {
			ctrlC, ctrlD := []byte{3}, []byte{4}
			buff := make([]byte, 3)
			for {
				n, _ := client.channel.Read(buff)
				received := buff[:n]
				if bytes.Equal(received, ctrlC) || bytes.Equal(received, ctrlD) {
					client.Disconnect()
				}
			}
		}()

	}
	startSshServer(2200, newClientHandler)
}

func startSshServer(port int, clientHandler clientHandler) {
	config := buildSshConfig()

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Fatalf("Failed to listen on %d (%s)", port, err)
	}

	log.Printf("Listening on %d...", port)

	for {
		tcpConn, err := listener.Accept()

		if err != nil {
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}

		// Before use, a handshake must be performed on the incoming net.Conn.
		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, config)
		if err != nil {
			log.Printf("Failed to handshake (%s)", err)
			continue
		}

		log.Printf("New SSH connection from %s]", sshConn.RemoteAddr())

		// Discard all global out-of-band Requests
		go ssh.DiscardRequests(reqs)
		// Accept all channels
		go handleChannels(chans, clientHandler)
	}
}

func handleChannels(chans <-chan ssh.NewChannel, clientHandler clientHandler) {
	// Service the incoming Channel channel in go routine
	for newChannel := range chans {
		go handleChannel(newChannel, clientHandler)
	}
}

func handleChannel(newChannel ssh.NewChannel, clientHandler clientHandler) {
	if t := newChannel.ChannelType(); t != "session" {
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		return
	}

	connection, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept channel (%s)", err)
		return
	}

	resizes := make(chan ScreenSize)
	id, _ := uuid.NewV4()
	client := sshClient{connection, resizes, id}

	log.Printf("Client %s connected", client.id)

	go func() {
		clientHandler(client)
	}()

	go func() {
		for req := range requests {
			ok := false
			switch req.Type {
			case "shell":
				if len(req.Payload) == 0 {
					ok = true
				}
			case "pty-req":
				ok = true
				strlen := req.Payload[3]
				resizes <- parseDims(req.Payload[strlen+4:])
			case "window-change":
				resizes <- parseDims(req.Payload)
				continue
			}
			req.Reply(ok, nil)
		}
	}()
}

func parseDims(b []byte) ScreenSize {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return ScreenSize{w, h}
}

func buildSshConfig() *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	privateBytes, err := ioutil.ReadFile("id_rsa")
	if err != nil {
		log.Fatal("Failed to load private key (./id_rsa)")
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key")
	}

	config.AddHostKey(private)
	return config
}
