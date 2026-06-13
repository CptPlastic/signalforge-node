package api

import "testing"

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		uri        string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{uri: "s3://my-space", wantBucket: "my-space", wantPrefix: ""},
		{uri: "s3://my-space/signalforge/call-archive/", wantBucket: "my-space", wantPrefix: "signalforge/call-archive"},
		{uri: "https://example.com", wantErr: true},
		{uri: "s3://", wantErr: true},
	}
	for _, tc := range tests {
		bucket, prefix, err := parseS3URI(tc.uri)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseS3URI(%q) expected error", tc.uri)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseS3URI(%q) error: %v", tc.uri, err)
		}
		if bucket != tc.wantBucket || prefix != tc.wantPrefix {
			t.Fatalf("parseS3URI(%q) = (%q, %q), want (%q, %q)", tc.uri, bucket, prefix, tc.wantBucket, tc.wantPrefix)
		}
	}
}

func TestJoinS3URI(t *testing.T) {
	got := joinS3URI("my-space", "signalforge/call-archive", "2026-06-01")
	want := "s3://my-space/signalforge/call-archive/2026-06-01/"
	if got != want {
		t.Fatalf("joinS3URI() = %q, want %q", got, want)
	}
}
