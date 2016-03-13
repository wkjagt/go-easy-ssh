package easyssh

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"net"

	"github.com/nu7hatch/gouuid"
	"golang.org/x/crypto/ssh"
)

type clientHandler func(client SshClient)
type ScreenSize struct {
	Width, Height uint32
}

type SshClient struct {
	Channel ssh.Channel
	Resizes chan ScreenSize
	Id      *uuid.UUID
}

func (c *SshClient) Disconnect() {
	log.Printf("Client %s hanging up", c.Id)
	c.Channel.Write([]byte("bye\n\r"))
	c.Channel.Close()
}

func WaitForClients(port int, keypath string, clientHandler clientHandler) {
	config := buildSshConfig(keypath)

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
	client := SshClient{connection, resizes, id}

	log.Printf("Client %s connected", client.Id)

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

func buildSshConfig(keypath string) *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	privateBytes, err := ioutil.ReadFile(keypath)
	if err != nil {
		log.Fatalf("Failed to load private key (%s)", keypath)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key")
	}

	config.AddHostKey(private)
	return config
}
