package service

import "testing"

func TestNormalizeImageBase64PreservesCorrectPadding(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"YQ==", "YQ=="},
		{"YQ", "YQ=="},
		{"YWI=", "YWI="},
		{"YWI", "YWI="},
		{"YWJj", "YWJj"},
		{"data:image/png;base64,YQ==", "YQ=="},
		{"data:image/png;base64,YWI=", "YWI="},
		{"not-valid!", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := normalizeOpenAIImageBase64(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
