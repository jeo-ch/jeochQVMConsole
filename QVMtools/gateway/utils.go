package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrConnectionLost = errors.New("connection lost")

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
