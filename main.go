package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log"
	"net"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/crypto/ssh"
)

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		log.Fatalln(err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}

		// region Server config
		config := &ssh.ServerConfig{
			NoClientAuth: true,
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatalln(err)
		}
		hostKey, err := ssh.NewSignerFromKey(key)
		if err != nil {
			log.Fatalln(err)
		}
		config.AddHostKey(hostKey)
		// endregion

		_,
		newChannels,
		requests,
		err :=
			ssh.NewServerConn(conn, config)
		if err != nil {
			continue
		}
		go handleRequests(requests)
		go handleChannels(newChannels)
	}
}

func handleChannels(channels <-chan ssh.NewChannel) {
	for {
		newChannel, ok := <-channels
		if !ok {
			break
		}
		go handleChannel(newChannel)
	}
}

func handleChannel(newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Fatalln(err)
	}

	tty := false
	for {
		req, ok := <-requests
		if !ok {
			break
		}
		switch req.Type {
		case "pty-req":
			tty = true
			if req.WantReply {
				req.Reply(true, []byte{})
			}
		case "shell":
			if req.WantReply {
				req.Reply(true, []byte{})
			}
			go launchShell(channel, tty)
		default:
			if req.WantReply {
				req.Reply(false, []byte{})
			}
		}
	}
}

func launchShell(channel ssh.Channel, tty bool) {
	cli, err := client.NewClientWithOpts()
	if err != nil {
		log.Fatalln(err)
	}
	cnt, err := cli.ContainerCreate(
		context.TODO(),
		&container.Config{
			Image: "ubuntu",
			Tty: tty,
			AttachStdin:  true,
			StdinOnce:    true,
			OpenStdin:    true,
		},
		nil,
		nil,
		nil,
		"",
	)
	if err != nil {
		log.Fatalln(err)
	}

	attach, err := cli.ContainerAttach(
		context.Background(),
		cnt.ID,
		types.ContainerAttachOptions{
			Stream:     true,
			Stdin:      true,
			Stdout:     true,
			Stderr:     true,
			Logs:       true,
		},
	)
	if err != nil {
		log.Fatalln(err)
	}

	err = cli.ContainerStart(context.TODO(), cnt.ID, types.ContainerStartOptions{})
	if err != nil {
		log.Fatalln(err)
	}

	closeChannel := func() {
		_ = channel.Close()
		_ = cli.ContainerRemove(context.TODO(), cnt.ID, types.ContainerRemoveOptions{})
	}

	var once sync.Once
	if tty {
		go func() {
			// Copy only to stdout
			_, _ = io.Copy(channel, attach.Reader)
			once.Do(closeChannel)
		}()
	} else {
		go func() {
			// Copy from to stdout and stderr
			_, _ = stdcopy.StdCopy(channel, channel.Stderr(), attach.Reader)
			once.Do(closeChannel)
		}()
	}
	go func() {
		// Copy stdin to container
		_, _ = io.Copy(attach.Conn, channel)
		once.Do(closeChannel)
	}()
}

func handleRequests(requests <-chan *ssh.Request) {
	for {
		request, ok := <-requests
		if !ok {
			break
		}
		if request.WantReply {
			_ = request.Reply(false, []byte("request type not supported"))
		}
	}
}
