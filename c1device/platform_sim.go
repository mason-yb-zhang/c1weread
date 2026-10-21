//go:build !linux || !mipsle

package c1device

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// Host builds talk to the c1sim supervisor when C1_SIM_ENDPOINT is set
// (host:port). The supervisor owns the web panel, desktop, and process tree.

type simPlatform struct {
	conn    net.Conn
	output  chan Event
	last    Frame
	ready   bool
	close   sync.Once
	writeMu sync.Mutex
	done    chan struct{}
}

type simMessage struct {
	T      string `json:"t"`
	App    string `json:"app,omitempty"`
	Title  string `json:"title,omitempty"`
	Key    string `json:"key,omitempty"`
	Rune   string `json:"rune,omitempty"`
	Repeat bool   `json:"repeat,omitempty"`
	Full   bool   `json:"full,omitempty"`
	FB     string `json:"fb,omitempty"`
	Line   string `json:"line,omitempty"`
	Code   int    `json:"code,omitempty"`
}

func openSimPlatform(endpoint string) (Platform, error) {
	conn, err := net.Dial("tcp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect simulator %s: %w", endpoint, err)
	}
	platform := &simPlatform{
		conn:   conn,
		output: make(chan Event, 32),
		done:   make(chan struct{}),
	}
	title := os.Getenv("C1_SIM_TITLE")
	if title == "" {
		title = "app"
	}
	name := os.Getenv("C1_SIM_APP")
	if name == "" {
		name = "app"
	}
	if err := platform.send(simMessage{T: "hello", App: name, Title: title}); err != nil {
		conn.Close()
		return nil, err
	}
	go platform.readLoop()
	return platform, nil
}

func (platform *simPlatform) send(message simMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	platform.writeMu.Lock()
	defer platform.writeMu.Unlock()
	data = append(data, '\n')
	_, err = platform.conn.Write(data)
	return err
}

func (platform *simPlatform) readLoop() {
	defer close(platform.done)
	defer close(platform.output)
	scanner := bufio.NewScanner(platform.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var message simMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		switch message.T {
		case "key":
			event := Event{Key: parseSimKey(message.Key), Repeat: message.Repeat}
			if event.Key == KeyRune && message.Rune != "" {
				for _, r := range message.Rune {
					event.Rune = r
					break
				}
			}
			select {
			case platform.output <- event:
			default:
			}
		case "shutdown":
			return
		}
	}
}

func parseSimKey(name string) Key {
	switch name {
	case "up":
		return KeyUp
	case "down":
		return KeyDown
	case "left":
		return KeyLeft
	case "right":
		return KeyRight
	case "ok":
		return KeyOK
	case "back":
		return KeyBack
	case "pause":
		return KeyPause
	case "volume-up":
		return KeyVolumeUp
	case "volume-down":
		return KeyVolumeDown
	case "rune":
		return KeyRune
	default:
		return KeyUnknown
	}
}

func (platform *simPlatform) Draw(next Frame, full bool) error {
	if platform.ready && next == platform.last && !full {
		return nil
	}
	full = full || !platform.ready
	message := simMessage{
		T:    "draw",
		Full: full,
		FB:   base64.StdEncoding.EncodeToString(next[:]),
	}
	if err := platform.send(message); err != nil {
		return fmt.Errorf("simulator draw: %w", err)
	}
	platform.last, platform.ready = next, true
	return nil
}

func (platform *simPlatform) Events() <-chan Event { return platform.output }

func (platform *simPlatform) Close() error {
	platform.close.Do(func() {
		_ = platform.send(simMessage{T: "exit", Code: 0})
		_ = platform.conn.Close()
		<-platform.done
	})
	return nil
}
