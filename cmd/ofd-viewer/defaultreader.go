package main

type defaultReaderState uint8

const (
	defaultReaderUnavailable defaultReaderState = iota
	defaultReaderUnknown
	defaultReaderNotDefault
	defaultReaderIsDefault
)

type defaultReaderResult struct {
	state defaultReaderState
	err   error
	set   bool
}
