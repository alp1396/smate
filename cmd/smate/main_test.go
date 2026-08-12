package main

import "testing"

func TestParseFlagOrder(t *testing.T) {
	for _, args := range [][]string{
		{"123", "--image", "smate-dev"},
		{"--image", "smate-dev", "123"},
		{"123", "--image=smate-dev"},
	} {
		id, flags, err := parse(args, []string{"image"}, nil)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if id != "123" || flags["image"] != "smate-dev" {
			t.Errorf("%v: got id=%q image=%q", args, id, flags["image"])
		}
	}
}

func TestParseBoolAndErrors(t *testing.T) {
	id, flags, err := parse([]string{"--purge"}, nil, []string{"purge"})
	if err != nil || id != "" || flags["purge"] != "true" {
		t.Fatalf("got id=%q flags=%v err=%v", id, flags, err)
	}
	if _, _, err := parse([]string{"a", "b"}, nil, nil); err == nil {
		t.Error("two positional arguments should be an error")
	}
	if _, _, err := parse([]string{"--wat"}, nil, nil); err == nil {
		t.Error("an unknown flag should be an error")
	}
	if _, _, err := parse([]string{"--image"}, []string{"image"}, nil); err == nil {
		t.Error("a valued flag without its value should be an error")
	}
}
