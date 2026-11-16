package core

import "testing"

func TestParseKeyInput(t *testing.T) {
	ok := map[string]string{
		"short":                                               "short",
		"AbCdEf123456":                                        "AbCdEf123456",
		"  AbCdEf123456\n":                                    "AbCdEf123456",
		"meridian://key/AbCdEf123456":                         "AbCdEf123456",
		"MERIDIAN://key/AbCdEf123456/":                        "AbCdEf123456",
		"meridian://key?k=AbCdEf123456":                       "AbCdEf123456",
		"meridian://KEY/AbCd%2BEf123456":                      "AbCd+Ef123456",
		" meridian://key/AbCdEf123456?utm=x ":                 "AbCdEf123456",
		"meridian://62.76.231.231:56000:56001:9000:stendKEY1": "stendKEY1",
		"meridian://62.76.231.231:56000:56001:9000:stendKEY1:vkhashEXAMPLE": "stendKEY1",
		"wdtt://62.76.231.231:56000:56001:9000:stendKEY1":                   "stendKEY1",
		"wdtt://key/stendKEY1": "stendKEY1",
	}
	for in, want := range ok {
		if got, good := ParseKeyInput(in); !good || got != want {
			t.Errorf("%q: %q %v, хочу %q", in, got, good, want)
		}
	}
	for _, in := range []string{
		"",
		"meridian://62.76.231.231:56000:56001:stendKEY1",
		"meridian://62.76.231.231:abc:56001:9000:stendKEY1",
		"meridian://62.76.231.231:56000:56001:9000:",
		"meridian://62.76.231.231:56000:56001:9000:stendKEY1:vk:extra",
		"wdtt://other/AbCdEf123456",
		"meridian://other/AbCdEf123456",
		"meridian://key/",
		"meridian://key/a/b/AbCdEf123456",
		"https://key/AbCdEf123456",
		"Ab Cd Ef 123456",
	} {
		if got, good := ParseKeyInput(in); good {
			t.Errorf("%q принят как %q", in, got)
		}
	}
}

func TestMaskKey(t *testing.T) {
	if got := MaskKey("AbCdEf123456"); got != "…3456" {
		t.Fatal(got)
	}
	if got := MaskKey("abc"); got != "…" {
		t.Fatal(got)
	}
}
