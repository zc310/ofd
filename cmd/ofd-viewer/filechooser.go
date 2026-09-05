package main

import "io"

type fileSelection struct {
	path   string
	name   string
	input  any
	output io.WriteCloser
}
