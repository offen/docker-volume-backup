package main

import "io"

type passThroughWriteCloser struct {
	target io.WriteCloser
}

func (p *passThroughWriteCloser) Write(b []byte) (int, error) {
	return p.target.Write(b)
}

func (p *passThroughWriteCloser) Close() error {
	return nil
}
