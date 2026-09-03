package main

import "testing"

func TestParseArgsSupportsTextFormat(t *testing.T) {
	opts, err := parseArgs([]string{"-format", "txt", "input.ofd", "output.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "txt" {
		t.Fatalf("format = %q, want txt", opts.format)
	}
	if opts.output != "output.txt" {
		t.Fatalf("output = %q, want output.txt", opts.output)
	}
}

func TestFormatFromExtensionSupportsText(t *testing.T) {
	if got := formatFromExtension("output.txt"); got != "txt" {
		t.Fatalf("format = %q, want txt", got)
	}
}
