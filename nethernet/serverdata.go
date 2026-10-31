package nethernet

import (
	"bytes"
	"errors"
	"fmt"
)

// ServerData is the decoded application data of a discovery response (version 7).
// The relay forwards the raw bytes; this decoder exists for diagnostics only.
type ServerData struct {
	Version               uint8
	ServerName            string
	Protocol              int32
	GameVersion           string
	LevelName             string
	PlayerCount           int32
	MaxPlayerCount        int32
	GameType              int32
	EditorWorld           bool
	Hardcore              bool
	AcceptsOnlineAuth     bool
	AcceptsSelfSignedAuth bool
	Nonce                 string
	ConnectionType        int32
}

// ParseServerData decodes a ServerData blob.
func ParseServerData(b []byte) (*ServerData, error) {
	r := bytes.NewReader(b)
	d := &ServerData{}
	var err error
	if d.Version, err = r.ReadByte(); err != nil {
		return nil, err
	}
	if d.ServerName, err = readString(r); err != nil {
		return nil, fmt.Errorf("server name: %w", err)
	}
	if d.Protocol, err = readVarint32(r); err != nil {
		return nil, fmt.Errorf("protocol: %w", err)
	}
	if d.GameVersion, err = readString(r); err != nil {
		return nil, fmt.Errorf("version: %w", err)
	}
	if d.LevelName, err = readString(r); err != nil {
		return nil, fmt.Errorf("level name: %w", err)
	}
	for _, f := range []*int32{&d.PlayerCount, &d.MaxPlayerCount, &d.GameType} {
		if *f, err = readVarint32(r); err != nil {
			return nil, err
		}
	}
	for _, f := range []*bool{&d.EditorWorld, &d.Hardcore, &d.AcceptsOnlineAuth, &d.AcceptsSelfSignedAuth} {
		c, readErr := r.ReadByte()
		if readErr != nil {
			return nil, readErr
		}
		*f = c != 0
	}
	if d.Nonce, err = readString(r); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	if d.ConnectionType, err = readVarint32(r); err != nil {
		return nil, fmt.Errorf("connection type: %w", err)
	}
	return d, nil
}

func readVaruint32(r *bytes.Reader) (uint32, error) {
	var v uint32
	for i := uint(0); i < 35; i += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << i
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("varuint32 too long")
}

func readVarint32(r *bytes.Reader) (int32, error) {
	u, err := readVaruint32(r)
	if err != nil {
		return 0, err
	}
	v := int32(u >> 1)
	if u&1 != 0 {
		v = ^v
	}
	return v, nil
}

func readString(r *bytes.Reader) (string, error) {
	n, err := readVaruint32(r)
	if err != nil {
		return "", err
	}
	if int(n) > r.Len() {
		return "", fmt.Errorf("length %d exceeds remaining %d", n, r.Len())
	}
	b := make([]byte, n)
	_, _ = r.Read(b)
	return string(b), nil
}
