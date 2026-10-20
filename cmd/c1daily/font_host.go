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
		return nil, fmt.Errorf("font file changed while reading")
	}
	return data, nil
}

func releaseFontData(data []byte) error {
	return nil
}
