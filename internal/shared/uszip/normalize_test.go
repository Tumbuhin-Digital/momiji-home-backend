package uszip

import "testing"

func TestNormalizeUSZip(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantOK  bool
	}{
		{name: "five digit", input: "94104", want: "94104", wantOK: true},
		{name: "trimmed", input: " 94104 ", want: "94104", wantOK: true},
		{name: "zip plus four hyphen", input: "94104-1234", want: "94104", wantOK: true},
		{name: "zip plus four space", input: "94104 1234", want: "94104", wantOK: true},
		{name: "letters", input: "abc", want: "", wantOK: false},
		{name: "too short", input: "9410", want: "", wantOK: false},
		{name: "empty", input: "", want: "", wantOK: false},
		{name: "digits then letters", input: "9410a", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeUSZip(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("NormalizeUSZip(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("NormalizeUSZip(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
