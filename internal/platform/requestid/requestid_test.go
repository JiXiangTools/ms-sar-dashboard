package requestid

import "testing"

func TestNormalizeUsesExactlySixteenCharacters(t *testing.T) {
	tests := map[string]string{
		"cmp-rec-hot-1":           "000cmp-rec-hot-1",
		"  abc  ":                 "0000000000000abc",
		"0123456789abcdef":        "0123456789abcdef",
		"prefix-0123456789abcdef": "0123456789abcdef",
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q)=%q want=%q", input, got, want)
		}
	}
}
