//go:build mage
// +build mage

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"sync"
)

func fileObjExists(s string) bool {
	_, err := os.Stat(s)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err)
	}

	return true
}

func dirExists(s string) bool {
	inf, err := os.Stat(s)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err)
	}

	return inf.IsDir()
}

type fileSig struct {
	rwm sync.RWMutex
	sig string
}

func (fs *fileSig) Get() string {
	fs.rwm.RLock()
	defer fs.rwm.RUnlock()

	return fs.sig
}

func (fs *fileSig) Set(s string) {
	fs.rwm.Lock()
	defer fs.rwm.Unlock()

	fs.sig = s
}

func (fs *fileSig) ComputeSig(fname string) error {
	h := sha256.New()
	f, err := os.Open(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	sum := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	fs.Set(sum)

	return nil
}
