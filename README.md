# go-easy-ssh

Just playing with Go and SSH. This package provides a server with no authentication, which makes it usage just like that of a telnet server.

### use like this

```go
package main

import (
	"bytes"
	"flag"
	"fmt"

	"github.com/wkjagt/go-easy-ssh"
)

func main() {
	newClientHandler := func(client easyssh.SshClient) {
		client.Channel.Write([]byte("welcome!"))

		// handle resizes
		go func() {
			for size := range client.Resizes {
				message := fmt.Sprintf("you resized your screen to %d x %d\n\r", size.Width, size.Height)
				client.Channel.Write([]byte(message))
			}
		}()

		// handle incoming bytes
		go func() {
			ctrlC, ctrlD := []byte{3}, []byte{4}
			buff := make([]byte, 3)
			for {
				n, _ := client.Channel.Read(buff)
				received := buff[:n]
				if bytes.Equal(received, ctrlC) || bytes.Equal(received, ctrlD) {
					client.Disconnect()
				}
			}
		}()

	}
	easyssh.WaitForClients(port, keypath, newClientHandler)
}
```
