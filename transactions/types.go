package transactions

import "strings"

type Type string

const (
	Unknown      Type = "Unknown"
	General      Type = "General"
	FtSetup      Type = "FtSetup"
	FtTransfer   Type = "FtTransfer"
	NftSetup     Type = "NftSetup"
	NftTransfer  Type = "NftTransfer"
	ArtDropSetup Type = "ArtDropSetup"
)

func (s Type) String() string {
	return string(s)
}

func (s Type) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *Type) UnmarshalText(text []byte) error {
	*s = StatusFromText(string(text))
	return nil
}

func StatusFromText(text string) Type {
	switch strings.ToLower(text) {
	default:
		return Unknown
	case "general":
		return General
	case "ftsetup":
		return FtSetup
	case "fttransfer":
		return FtTransfer
	case "nftsetup":
		return NftSetup
	case "nfttransfer":
		return NftTransfer
	case "artdropsetup":
		return ArtDropSetup
	}
}
