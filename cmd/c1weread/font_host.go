//go:build !linux

package main

import (
	"fmt"
	"io"
	"os"
)

func openFontFile(path string) (*os.File, error) {
	return os.Open(path)
}

func loadFontData(file *os.File, size int) ([]byte, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, err := file.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, fmt.Errorf("字体文件在读取时发生变化")
	}
	return data, nil
}

func releaseFontData(data []byte) error {
	return nil
}
