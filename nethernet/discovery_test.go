package nethernet

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, in := range []*Packet{
		{ID: IDRequest, SenderID: 42},
		{ID: IDResponse, SenderID: 7, ApplicationData: []byte{0x07, 0x01, 0xff, 0x00}},
		{ID: IDMessage, SenderID: 1, RecipientID: 2, Data: "CONNECTREQUEST 123 v=0"},
	} {
		out, err := Unmarshal(Marshal(in))
		if err != nil {
			t.Fatalf("id %d: %v", in.ID, err)
		}
		if out.ID != in.ID || out.SenderID != in.SenderID || out.RecipientID != in.RecipientID ||
			out.Data != in.Data || !bytes.Equal(out.ApplicationData, in.ApplicationData) {
			t.Errorf("round trip mismatch: got %+v want %+v", out, in)
		}
	}
}

// TestMessageUndeclaredTail checks that data beyond the declared length prefix
// (as written by vanilla clients) is kept instead of truncated.
func TestMessageUndeclaredTail(t *testing.T) {
	const full = "CONNECTREQUEST 99 v=0 the rest of the sdp"
	const declared = 10

	body := binary.LittleEndian.AppendUint16(nil, IDMessage)
	body = binary.LittleEndian.AppendUint64(body, 1) // sender
	body = append(body, make([]byte, 8)...)
	body = binary.LittleEndian.AppendUint64(body, 2) // recipient
	body = binary.LittleEndian.AppendUint32(body, declared)
	body = append(body, full...)
	payload := append(binary.LittleEndian.AppendUint16(nil, uint16(len(body)+2)), body...)

	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	pk, err := Unmarshal(append(mac.Sum(nil), encryptECB(payload)...))
	if err != nil {
		t.Fatal(err)
	}
	if pk.Data != full {
		t.Fatalf("got %q, want %q", pk.Data, full)
	}
}

func TestTamperedChecksum(t *testing.T) {
	b := Marshal(&Packet{ID: IDRequest, SenderID: 1})
	b[0] ^= 0xff
	if _, err := Unmarshal(b); err == nil {
		t.Fatal("expected checksum error")
	}
}

func TestGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, make([]byte, 10), make([]byte, 64), make([]byte, 80)} {
		if _, err := Unmarshal(b); err == nil {
			t.Errorf("expected error for %d bytes", len(b))
		}
	}
}
