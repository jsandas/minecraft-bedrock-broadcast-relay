package nethernet

import "testing"

func TestRewriteCandidates(t *testing.T) {
	in := "CANDIDATEADD 55 candidate:1 1 udp 2122260223 172.17.0.2 19132 typ host generation 0 ufrag abcd network-id 1"
	want := "CANDIDATEADD 55 candidate:1 1 udp 2122260223 10.4.10.50 19132 typ host generation 0 ufrag abcd network-id 1"
	got, n := RewriteCandidates(in, "10.4.10.50")
	if got != want || n != 1 {
		t.Fatalf("got %q (%d), want %q", got, n, want)
	}
	if _, n := RewriteCandidates(got, "10.4.10.50"); n != 0 {
		t.Fatalf("already-rewritten candidate counted: %d", n)
	}
	sdp := "v=0\r\na=candidate:2 1 udp 1 172.17.0.2 5000 typ host\r\na=mid:0\r\n"
	if got, n := RewriteCandidates(sdp, "1.2.3.4"); n != 1 || got != "v=0\r\na=candidate:2 1 udp 1 1.2.3.4 5000 typ host\r\na=mid:0\r\n" {
		t.Fatalf("sdp rewrite: %q (%d)", got, n)
	}
}
