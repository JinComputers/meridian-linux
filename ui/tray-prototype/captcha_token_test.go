package main

import "testing"

func TestFindCaptchaToken(t *testing.T) {
	ok := map[string]string{
		"https://id.vk.ru/ok?success_token=abcDEF12345&x=1":             "abcDEF12345",
		`{"response":{"success_token":"tok_1234567890"}}`:               "tok_1234567890",
		`{"success_token": "tok_ABC12345"}`:                             "tok_ABC12345",
		"https://x/?success_token=a%2Bb%2Fc12345":                       "a+b/c12345",
		"https://x/?success_token=short&y=1&success_token=longEnough99": "longEnough99",
		"текст ... success_token=tok12345678 ... дальше":                "tok12345678",
	}
	for in, want := range ok {
		if got, good := findCaptchaToken(in); !good || got != want {
			t.Errorf("%q: %q %v, хочу %q", in, got, good, want)
		}
	}
	for _, in := range []string{"", "no token here", "success_token=", "success_token=abc&", `"success_token":""`} {
		if got, good := findCaptchaToken(in); good {
			t.Errorf("%q принят как %q", in, got)
		}
	}
}

func TestCaptchaURLAllowed(t *testing.T) {
	if !captchaURLAllowed("https://id.vk.ru/not_robot_captcha?x=1") {
		t.Error("https отклонён")
	}
	for _, u := range []string{"http://id.vk.ru/x", "file:///c:/x", "javascript:alert(1)", "https://", ""} {
		if captchaURLAllowed(u) {
			t.Errorf("%q разрешён", u)
		}
	}
}
