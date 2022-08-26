package tools

import (
	"encoding/hex"
	"testing"

	"github.com/go-test/deep"
)

func TestMessageHex(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect string
	}{
		{
			name:   "common header",
			input:  []byte{3, 0, 0, 0, 32, 4},
			expect: "[ 0x03, 0x00, 0x00, 0x00, 0x20, 0x04 ]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MessageHex(tt.input)
			if diff := deep.Equal(tt.expect, got); diff != nil {
				t.Errorf("%+v", diff)
			}
		})
	}
} 
func TestConvertToHex(t *testing.T) {
	for b := 0; b <= 0xff; b++ {
		s := ConvertToHex(byte(b))
		v, _ := hex.DecodeString(s)
		if int(v[0]) != b {
			t.Errorf("original %d and decoded %d values do not match", b, v)
		}
	}
}

func TestHostAddrValidator(t *testing.T) {
    tests := []struct {
        name string
        input string
        fail bool
    }{
        {
	        name: "valid ipv4 and port",
	        input: "1.1.1.1:8080",
	        fail: false,
        },
        {
	        name: "valid ipv4 and mo port",
	        input: "1.1.1.1",
	        fail: true,
        },
        {
	        name: "valid dns and port",
	        input: "localhost:8080",
	        fail: false,
        },
        {
	        name: "valid dns and no port",
	        input: "localhost",
	        fail: true,
        },
        {
	        name: "invalid dns; valid port",
	        input: "gbbbbbbbbbbb.com:8080",
	        fail: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := HostAddrValidator(tt.input)
            if err != nil && !tt.fail {
                t.Fatalf("supposed to succeed but fail with error: %+v", err)
            }
            if err == nil && tt.fail {
                t.Fatalf("supposed to fail but succeeded")
            }
        })
    }
}

func TestURLAddrValidation(t *testing.T) {
    tests := []struct {
        name string
        input string
        fail bool
    }{
        {
	        name: "valid URL with ipv4 and port",
	        input: "http://1.1.1.1:8080",
	        fail: false,
        },
        {
	        name: "invalid URL",
	        input: "1.1.1.1:8080",
	        fail: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := URLAddrValidation(tt.input)
            if err != nil && !tt.fail {
                t.Fatalf("supposed to succeed but fail with errorL %+v", err)
            }
            if err == nil && tt.fail {
                t.Fatalf("supposed to fail but succeeded")
            }
        })
    }
}
