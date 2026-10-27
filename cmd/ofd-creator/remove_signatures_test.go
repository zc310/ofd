package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRemoveSignaturesCLIRejectsArgumentsBeforeReadingInput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-o", "out.ofd"}, {"-i", "missing.ofd"},
		{"-i", "-", "-o", "out.ofd"},
		{"-i", "missing.ofd", "-o", "missing.ofd"},
		{"-i", "missing.ofd", "-o", "out.ofd", "--id", "../bad"},
		{"-i", "missing.ofd", "-o", "out.ofd", "--id", "s1", "--cleanup"},
		{"-i", "missing.ofd", "-o", "out.ofd", "other.ofd"},
		{"-o", "out.ofd", "first.ofd", "second.ofd"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"remove-signatures"}, args...), &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"remove-signatures", "--help"}, &stdout, &stderr); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("帮助失败: %d %s", code, &stderr)
	}
	for _, flag := range []string{"--id", "--cleanup", "--validate", "--output"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("帮助缺少 %s: %s", flag, &stdout)
		}
	}
}
