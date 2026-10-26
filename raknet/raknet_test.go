package raknet

import (
	"bytes"
	"testing"
)

func TestUnconnectedPing(t *testing.T) {
	ping := &UnconnectedPing{
		SendTimestamp: 123456789,
		Magic:         unconnectedMessageSequence,
		ClientGUID:    987654321,
	}

	raw := make([]byte, 33)
	raw[0] = IDUnconnectedPing
	// Marshal and unmarshal test
	var unmarshaled UnconnectedPing
	buf := make([]byte, 33)
	buf[0] = IDUnconnectedPing
	copy(buf[9:], ping.Magic[:])

	// Using the binary packet format
	testPacket := []byte{
		IDUnconnectedPing,
		0x00, 0x00, 0x00, 0x00, 0x07, 0x5b, 0xcd, 0x15, // 123456789
		0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78,
		0x00, 0x00, 0x00, 0x00, 0x3a, 0xde, 0x68, 0xb1, // 987654321
	}

	if err := unmarshaled.UnmarshalBinary(testPacket); err != nil {
		t.Fatalf("failed to unmarshal ping: %v", err)
	}

	if unmarshaled.SendTimestamp != ping.SendTimestamp {
		t.Errorf("expected timestamp %d, got %d", ping.SendTimestamp, unmarshaled.SendTimestamp)
	}
	if unmarshaled.ClientGUID != ping.ClientGUID {
		t.Errorf("expected client GUID %d, got %d", ping.ClientGUID, unmarshaled.ClientGUID)
	}
}

func TestUnconnectedPongRoundTrip(t *testing.T) {
	original := &UnconnectedPong{
		SendTimestamp: 123456789,
		ServerGUID:    987654321,
		Magic:         unconnectedMessageSequence,
		Data:          []byte("MCPE;Bedrock Server;527;1.19.50;0;20;123456;Survival;1;19132;19133;"),
	}

	data := original.MarshalBinary()
	if data[0] != IDUnconnectedPong {
		t.Fatalf("expected packet ID 0x1c, got 0x%x", data[0])
	}

	var parsed UnconnectedPong
	if err := parsed.UnmarshalBinary(data); err != nil {
		t.Fatalf("failed to unmarshal pong: %v", err)
	}

	if parsed.SendTimestamp != original.SendTimestamp {
		t.Errorf("expected timestamp %d, got %d", original.SendTimestamp, parsed.SendTimestamp)
	}
	if parsed.ServerGUID != original.ServerGUID {
		t.Errorf("expected server GUID %d, got %d", original.ServerGUID, parsed.ServerGUID)
	}
	if !bytes.Equal(parsed.Data, original.Data) {
		t.Errorf("expected data %q, got %q", string(original.Data), string(parsed.Data))
	}
}
