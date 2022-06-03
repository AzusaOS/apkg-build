package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"log"
	"os"

	"golang.org/x/crypto/sha3"
)

var hashes = map[string]func() hash.Hash{
	"sha1":     sha1.New,
	"sha256":   sha256.New,
	"sha3-256": sha3.New256,
}

func hashFile(fn string) map[string]string {
	f, err := os.Open(fn)
	if err != nil {
		log.Printf("failed to open %s: %s", fn, err)
		return nil
	}
	defer f.Close()

	calc := make(map[string]hash.Hash)
	var wr []io.Writer

	for hashName, factory := range hashes {
		h := factory()
		calc[hashName] = h
		wr = append(wr, h)
	}

	_, err = io.Copy(io.MultiWriter(wr...), f)
	if err != nil {
		log.Printf("failed to read %s: %s", fn, err)
	}

	res := make(map[string]string)

	for hashName, h := range calc {
		res[hashName] = hex.EncodeToString(h.Sum(nil))
	}

	return res
}
