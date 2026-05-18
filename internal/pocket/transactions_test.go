package pocket

import (
	"reflect"
	"testing"
)

func TestSetOrAppendFlag(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		flag  string
		value string
		want  []string
	}{
		{
			name:  "append when absent",
			args:  []string{"tx", "bank", "send", "--node", "rpc"},
			flag:  "--sequence",
			value: "5",
			want:  []string{"tx", "bank", "send", "--node", "rpc", "--sequence", "5"},
		},
		{
			name:  "replace when present",
			args:  []string{"tx", "bank", "send", "--sequence", "3", "--node", "rpc"},
			flag:  "--sequence",
			value: "5",
			want:  []string{"tx", "bank", "send", "--sequence", "5", "--node", "rpc"},
		},
		{
			name:  "replace last when duplicate",
			args:  []string{"--sequence", "1", "--node", "rpc", "--sequence", "2"},
			flag:  "--sequence",
			value: "9",
			want:  []string{"--sequence", "9", "--node", "rpc", "--sequence", "9"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := setOrAppendFlag(c.args, c.flag, c.value)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("\n got: %v\nwant: %v", got, c.want)
			}
		})
	}
}

func TestParseExpectedSequence(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		want     uint64
		wantOk   bool
	}{
		{
			name:   "live pocketd error",
			input:  `pocketd command failed: ... rpc error: code = Unknown desc = account sequence mismatch, expected 987, got 986: incorrect account sequence [cosmos/cosmos-sdk@v0.53.0/x/auth/ante/sigverify.go:364] with gas used: '28978': unknown request`,
			want:   987,
			wantOk: true,
		},
		{
			name:   "minimal form",
			input:  `account sequence mismatch, expected 1, got 0`,
			want:   1,
			wantOk: true,
		},
		{
			name:   "large numbers",
			input:  `account sequence mismatch, expected 1234567890, got 1234567889: incorrect account sequence`,
			want:   1234567890,
			wantOk: true,
		},
		{
			name:   "no match",
			input:  `pocketd command failed: insufficient funds`,
			want:   0,
			wantOk: false,
		},
		{
			name:   "empty",
			input:  "",
			want:   0,
			wantOk: false,
		},
		{
			name:   "partial keyword no number",
			input:  `account sequence mismatch but no numbers`,
			want:   0,
			wantOk: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseExpectedSequence(c.input)
			if ok != c.wantOk {
				t.Errorf("ok = %v, want %v", ok, c.wantOk)
			}
			if got != c.want {
				t.Errorf("got = %d, want %d", got, c.want)
			}
		})
	}
}
