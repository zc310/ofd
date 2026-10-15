package config

import "testing"

func TestImageOptionsNormalizeDefaultsAndValidate(t *testing.T) {
	options, err := (Options{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if options.ImageMode != ImageModeCompatible || options.JPEGQuality != DefaultJPEGQuality {
		t.Fatalf("defaults: mode=%q quality=%d", options.ImageMode, options.JPEGQuality)
	}
	options, err = (Options{ImageMode: ImageModeCompact, JPEGQuality: 72}).Normalize()
	if err != nil || options.ImageMode != ImageModeCompact || options.JPEGQuality != 72 {
		t.Fatalf("compact options=%+v err=%v", options, err)
	}
	for _, invalid := range []Options{
		{ImageMode: "source"},
		{JPEGQuality: -1},
		{JPEGQuality: 101},
	} {
		if _, err := invalid.Normalize(); err == nil {
			t.Fatalf("accepted invalid image options %+v", invalid)
		}
	}
}
