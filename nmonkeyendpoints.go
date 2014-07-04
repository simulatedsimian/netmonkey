package main

import (
	"fmt"
	"github.com/simulatedsimian/neo"
	"github.com/tarm/goserial"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
)

var devNULL neo.NullReaderWriterCloser

type EndPoint interface {
	io.Reader
	io.Writer
	io.Closer
	neo.Named
}

type EndPointImpl struct {
	io.Reader
	io.Writer
	io.Closer
	name string
}

func (e *EndPointImpl) Name() string {
	return e.name
}

type Echoer struct {
	buffchan chan ([]byte)
	readbuf  []byte
}

func MakeEchoEndPoint() *Echoer {
	return &Echoer{make(chan []byte), nil}
}

func (e *Echoer) Read(p []byte) (int, error) {

	if len(e.readbuf) == 0 {
		e.readbuf = <-e.buffchan
	}

	if len(e.readbuf) > 0 {
		n := copy(p, e.readbuf)
		e.readbuf = e.readbuf[n:]
		return n, nil
	}

	return 0, nil
}

func (e *Echoer) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	e.buffchan <- b
	return len(p), nil
}

type EndpointMaker func(name, config string, epch chan (EndPoint), errch chan (error))

var endPointFactory = map[string]EndpointMaker{

	"echo": func(name, config string, epch chan (EndPoint), errch chan (error)) {

		echo := MakeEchoEndPoint()
		epch <- &EndPointImpl{echo, echo, devNULL, name}
	},

	"null": func(name, config string, epch chan (EndPoint), errch chan (error)) {

		epch <- &EndPointImpl{devNULL, devNULL, devNULL, name}
	},

	"socketConnect": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		conn, err := net.Dial("tcp", config)

		if err == nil {
			epch <- &EndPointImpl{conn, conn, conn, name}
		} else {
			errch <- err
		}
	},

	"socketListen": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		listener, err := net.Listen("tcp", config)
		if err != nil {
			errch <- err
		} else {
			defer listener.Close()

			conn, err := listener.Accept()

			if err == nil {
				epch <- &EndPointImpl{conn, conn, conn, name}
			} else {
				errch <- err
			}
		}
	},

	"fileReader": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		f, err := os.Open(config)
		if err != nil {
			errch <- err
		} else {
			epch <- &EndPointImpl{f, devNULL, f, name}
		}
	},

	"fileWriter": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		f, err := os.Create(config)
		if err != nil {
			errch <- err
		} else {
			epch <- &EndPointImpl{devNULL, f, f, name}
		}
	},

	"fileAppender": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		f, err := os.OpenFile(config, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			errch <- err
		} else {
			epch <- &EndPointImpl{devNULL, f, f, name}
		}
	},

	"stdio": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		epch <- &EndPointImpl{os.Stdin, os.Stdout, devNULL, name}
	},

	"stderr": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		epch <- &EndPointImpl{devNULL, os.Stderr, devNULL, name}
	},

	"serialPort": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		serconf := &serial.Config{Name: config, Baud: 9600}
		s, err := serial.OpenPort(serconf)
		if err != nil {
			errch <- err
		} else {
			epch <- &EndPointImpl{s, s, s, name}
		}
	},

	"process": func(name, config string, epch chan (EndPoint), errch chan (error)) {
		s := strings.Fields(config)

		proc := exec.Command(s[0])

		if len(s) > 1 {
			proc.Args = append(proc.Args, s[1:]...)
		}

		ep := EndPointImpl{nil, nil, devNULL, name}

		w, err := proc.StdinPipe()
		if err != nil {
			errch <- err
			return
		}

		r, err := proc.StdoutPipe()
		if err != nil {
			errch <- err
			return
		}

		ep.Writer = w
		ep.Reader = r
		proc.Stderr = proc.Stdout

		epch <- &ep
		e := proc.Run()

		if e == nil {
			errch <- io.EOF
		} else {
			errch <- e
		}
	},
}

func CreateEndPoint(epi *EndPointInfo, epch chan EndPoint, errch chan error) error {
	log.Printf("Creating Endpoint: %s [%s(%s)]", epi.Name, epi.Kind, epi.Config)
	if maker, ok := endPointFactory[epi.Kind]; ok {
		go maker(epi.Name, epi.Config, epch, errch)
	} else {
		return neo.ErrorStr(fmt.Sprintf("Unknown EndPoint Type: %s", epi.Kind))
	}
	return nil
}

func CreateEndPoints(config []EndPointInfo, dependency string, epch chan EndPoint, errch chan error) error {

	for _, v := range config {
		if v.Depends == dependency {
			err := CreateEndPoint(&v, epch, errch)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
