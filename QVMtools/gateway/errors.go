package main

import "errors"

var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrTokenConsumed = errors.New("token already consumed")
	ErrTokenExpired  = errors.New("token expired")
)
