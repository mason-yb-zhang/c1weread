//go:build !linux || !mipsle

package c1device

import (
	"os"
	"sync"
)

type hostPlatform struct {
	output chan Event
	last   Frame
	close  sync.Once
}

func OpenPlatform() (Platform, error) {
	if endpoint := os.Getenv("C1_SIM_ENDPOINT"); endpoint != "" {
		return openSimPlatform(endpoint)
	}
	return &hostPlatform{output: make(chan Event, 32)}, nil
}

func (platform *hostPlatform) Draw(frame Frame, _ bool) error {
	platform.last = frame
	return nil
}

func (platform *hostPlatform) Events() <-chan Event { return platform.output }

func (platform *hostPlatform) Close() error {
	platform.close.Do(func() { close(platform.output) })
	return nil
}
