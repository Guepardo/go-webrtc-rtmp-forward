//go:build !js
// +build !js

package main

func main() {
	server := NewServer()
	server.Start()
}
