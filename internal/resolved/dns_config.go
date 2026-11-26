package resolved

import "bytes"

type dnsServer struct {
	Family     int32
	Address    []byte
	Port       uint16
	ServerName string
}

func dnsConfigMatches(got []dnsServer, want dnsServer) bool {
	if len(got) != 1 {
		return false
	}
	return got[0].Family == want.Family &&
		bytes.Equal(got[0].Address, want.Address) &&
		got[0].Port == want.Port &&
		got[0].ServerName == want.ServerName
}
