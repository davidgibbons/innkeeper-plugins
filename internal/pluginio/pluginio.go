// Package pluginio has the file helpers plugins share.
package pluginio

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// MaxInput caps the files plugins read. Card PNGs are a few MB at most.
const MaxInput = 32 << 20

// ReadFile reads the file at path, failing if it is over MaxInput.
func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadAll(f)
}

// ReadAll reads f, failing if it holds more than MaxInput bytes.
func ReadAll(f *os.File) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, MaxInput+1))
	if err == nil && len(b) > MaxInput {
		err = fmt.Errorf("%s is over %d MiB", f.Name(), MaxInput>>20)
	}
	return b, err
}

// WriteTmp writes b to a new file in tmp, the blob_tmp folder, and returns
// its path.
func WriteTmp(tmp, pattern string, b []byte) (string, error) {
	if tmp == "" {
		return "", errors.New("initialize sent no blob_tmp")
	}
	f, err := os.CreateTemp(tmp, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// ReadIn reads the file name inside root, failing if it is over MaxInput.
func ReadIn(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadAll(f)
}
