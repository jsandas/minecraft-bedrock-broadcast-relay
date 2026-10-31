// Package nethernet implements the minimal NetherNet LAN discovery packet codec
// needed by the relay (request/response/message packets on UDP 7551).
// The wire format is based on https://github.com/df-mc/nethernet-spec and the
// reference implementation in https://github.com/df-mc/go-nethernet.
package nethernet

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// DefaultPort is the UDP port used for LAN discovery.
const DefaultPort = 7551

// Discovery packet IDs.
const (
	IDRequest  uint16 = 0
	IDResponse uint16 = 1
	IDMessage  uint16 = 2
)

const (
	checksumLen = sha256.Size
	headerLen   = 2 + 8 + 8 // packet ID, sender ID, padding
	maxLen      = 1<<16 - 1
)

// key is the AES-256/HMAC key shared by all clients: SHA-256 of 0xdeadbeef (uint64, little endian).
var key = sha256.Sum256(binary.LittleEndian.AppendUint64(nil, 0xdeadbeef))

// Packet is a decoded discovery packet.
type Packet struct {
	ID       uint16
	SenderID uint64

	// ApplicationData is the decoded ServerData blob (IDResponse only).
	ApplicationData []byte

	// RecipientID and Data are set for IDMessage only.
	RecipientID uint64
	Data        string
}

// Marshal encodes and encrypts p. The checksum is an HMAC-SHA256 over the plaintext payload.
func Marshal(p *Packet) []byte {
	body := &bytes.Buffer{}
	_ = binary.Write(body, binary.LittleEndian, p.ID)
	_ = binary.Write(body, binary.LittleEndian, p.SenderID)
	body.Write(make([]byte, 8))

	switch p.ID {
	case IDResponse:
		enc := hex.EncodeToString(p.ApplicationData)
		_ = binary.Write(body, binary.LittleEndian, uint32(len(enc))) //nolint:gosec // bounded by UDP size
		body.WriteString(enc)
	case IDMessage:
		_ = binary.Write(body, binary.LittleEndian, p.RecipientID)
		_ = binary.Write(body, binary.LittleEndian, uint32(len(p.Data))) //nolint:gosec // bounded by UDP size
		body.WriteString(p.Data)
	}

	// The length prefix counts the whole decrypted payload, including itself.
	payload := binary.LittleEndian.AppendUint16(nil, uint16(body.Len()+2)) //nolint:gosec // bounded by UDP size
	payload = append(payload, body.Bytes()...)

	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	return append(mac.Sum(nil), encryptECB(payload)...)
}

// Unmarshal verifies, decrypts and decodes a discovery packet.
func Unmarshal(b []byte) (*Packet, error) {
	if len(b) < checksumLen+aes.BlockSize {
		return nil, errors.New("packet too short")
	}
	payload, err := decryptECB(b[checksumLen:])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), b[:checksumLen]) {
		return nil, errors.New("checksum mismatch")
	}
	if len(payload) < 2+headerLen {
		return nil, errors.New("payload too short")
	}
	r := payload[2:] // skip length prefix

	p := &Packet{
		ID:       binary.LittleEndian.Uint16(r[0:2]),
		SenderID: binary.LittleEndian.Uint64(r[2:10]),
	}
	r = r[headerLen:]

	switch p.ID {
	case IDRequest:
	case IDResponse:
		raw, err := readBlob(&r)
		if err != nil {
			return nil, fmt.Errorf("response: %w", err)
		}
		p.ApplicationData = make([]byte, hex.DecodedLen(len(raw)))
		if _, err := hex.Decode(p.ApplicationData, raw); err != nil {
			return nil, fmt.Errorf("response: decode application data: %w", err)
		}
	case IDMessage:
		if len(r) < 8 {
			return nil, errors.New("message: short")
		}
		p.RecipientID = binary.LittleEndian.Uint64(r[:8])
		r = r[8:]
		raw, err := readBlob(&r)
		if err != nil {
			return nil, fmt.Errorf("message: %w", err)
		}
		// Vanilla may write more data than the length prefix declares, with the
		// rest of the signal continuing right after the declared range.
		p.Data = string(raw) + string(r)
	default:
		return nil, fmt.Errorf("unknown packet ID %d", p.ID)
	}
	return p, nil
}

// readBlob reads a uint32 length-prefixed byte slice and advances r.
func readBlob(r *[]byte) ([]byte, error) {
	if len(*r) < 4 {
		return nil, errors.New("short length")
	}
	n := binary.LittleEndian.Uint32(*r)
	if n > maxLen || int(n) > len(*r)-4 {
		return nil, fmt.Errorf("invalid length %d", n)
	}
	b := (*r)[4 : 4+n]
	*r = (*r)[4+n:]
	return b, nil
}

// encryptECB encrypts src with AES-ECB and PKCS#7 padding.
func encryptECB(src []byte) []byte {
	block, _ := aes.NewCipher(key[:])
	pad := aes.BlockSize - len(src)%aes.BlockSize
	src = append(append([]byte(nil), src...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	dst := make([]byte, len(src))
	for i := 0; i < len(src); i += aes.BlockSize {
		block.Encrypt(dst[i:i+aes.BlockSize], src[i:i+aes.BlockSize])
	}
	return dst
}

// decryptECB decrypts src with AES-ECB and strips PKCS#7 padding.
func decryptECB(src []byte) ([]byte, error) {
	if len(src) == 0 || len(src)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext length %d", len(src))
	}
	block, _ := aes.NewCipher(key[:])
	dst := make([]byte, len(src))
	for i := 0; i < len(src); i += aes.BlockSize {
		block.Decrypt(dst[i:i+aes.BlockSize], src[i:i+aes.BlockSize])
	}
	pad := int(dst[len(dst)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(dst) {
		return nil, errors.New("invalid padding")
	}
	return dst[:len(dst)-pad], nil
}
