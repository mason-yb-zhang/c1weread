//go:build !linux || !mipsle

package c1device

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestSimPlatformDrawAndKeys(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("C1_SIM_ENDPOINT", listener.Addr().String())
	t.Setenv("C1_SIM_APP", "unit")
	t.Setenv("C1_SIM_TITLE", "unit-title")

	type wire struct {
		T    string `json:"t"`
		Key  string `json:"key"`
		Rune string `json:"rune"`
		FB   string `json:"fb"`
		Full bool   `json:"full"`
		App  string `json:"app"`
	}
	got := make(chan wire, 8)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var message wire
			if json.Unmarshal(scanner.Bytes(), &message) == nil {
				got <- message
			}
			if message.T == "hello" {
				_, _ = conn.Write([]byte(`{"t":"key","key":"up"}` + "\n"))
			}
		}
	}()

	platform, err := OpenPlatform()
	if err != nil {
		t.Fatal(err)
	}
	defer platform.Close()

	hello := <-got
	if hello.T != "hello" || hello.App != "unit" {
		t.Fatalf("hello = %+v", hello)
	}
	select {
	case event := <-platform.Events():
		if event.Key != KeyUp {
			t.Fatalf("event key = %v", event.Key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no key event")
	}

	var frame Frame
	frame[0] = 0xff
	if err := platform.Draw(frame, true); err != nil {
		t.Fatal(err)
	}
	drawn := <-got
	if drawn.T != "draw" || !drawn.Full {
		t.Fatalf("draw = %+v", drawn)
	}
	raw, err := base64.StdEncoding.DecodeString(drawn.FB)
	if err != nil || len(raw) != FrameBytes || raw[0] != 0xff {
		t.Fatalf("frame payload invalid len=%d err=%v", len(raw), err)
	}
}
