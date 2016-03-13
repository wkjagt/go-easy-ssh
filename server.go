package easyssh

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"github.com/nu7hatch/gouuid"
	"golang.org/x/crypto/ssh"
)

type clientHandler func(client *SshClient)
type ScreenSize struct {
	Width, Height uint32
}

type SshClient struct {
	Channel ssh.Channel
	Resizes chan *ScreenSize
	Id      *uuid.UUID
}

func (c *SshClient) Write(m string) {
	c.Channel.Write([]byte(m))
}

func (c *SshClient) Read(b []byte) (int, error) {
	return c.Channel.Read(b)
}

func (c *SshClient) Disconnect() {
	log.Printf("Client %s hanging up", c.Id)
	c.Channel.Write([]byte("bye\n\r"))
	c.Channel.Close()
}

func WaitForClients(port int, key []byte, clientHandler clientHandler) {
	config := buildSshConfig(key)

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

		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, config)
		if err != nil {
			log.Printf("Failed to handshake (%s)", err)
			continue
		}

		log.Printf("New SSH connection from %s]", sshConn.RemoteAddr())

		go ssh.DiscardRequests(reqs)
		go handleChannels(chans, clientHandler)
	}
}

func handleChannels(chans <-chan ssh.NewChannel, clientHandler clientHandler) {
	for newChannel := range chans {
		go handleChannel(newChannel, clientHandler)
	}
}

func newClient(connection ssh.Channel) *SshClient {
	resizes := make(chan *ScreenSize)
	id, _ := uuid.NewV4()
	return &SshClient{connection, resizes, id}
}

func handleChannel(newChannel ssh.NewChannel, clientHandler clientHandler) {
	if t := newChannel.ChannelType(); t != "session" {
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		return
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept channel (%s)", err)
		return
	}

	client := newClient(channel)

	log.Printf("Client %s connected", client.Id)

	go clientHandler(client)
	go handleRequests(requests, client)
}

func handleRequests(requests <-chan *ssh.Request, client *SshClient) {
	for req := range requests {
		switch req.Type {
		case "shell":
			req.Reply(len(req.Payload) == 0, nil)
		case "pty-req":
			strlen := req.Payload[3]
			client.Resizes <- parseDims(req.Payload[strlen+4:])
			req.Reply(true, nil)
		case "window-change":
			client.Resizes <- parseDims(req.Payload)
		}
	}
}

func parseDims(b []byte) *ScreenSize {
	return &ScreenSize{
		Width:  binary.BigEndian.Uint32(b),
		Height: binary.BigEndian.Uint32(b[4:]),
	}
}

func buildSshConfig(key []byte) *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	private, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Fatal("Failed to parse private key")
	}

	config.AddHostKey(private)
	return config
}
