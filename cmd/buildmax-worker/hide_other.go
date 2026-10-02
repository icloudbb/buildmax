//go:build !linux

package main

// hideFromSandbox is a no-op off Linux: no other backend re-binds /proc.
func hideFromSandbox() error { return nil }
