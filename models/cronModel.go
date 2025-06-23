package models

import "net/netip"

type HostInfo struct {
	Id             string
	Ip             netip.Addr
	Name           string
	TelnetUsername string
	TelnetPasswd   string
	SnmpCommunity  string
}

type ItemResult struct {
	ItemId string
	Value  string
	Table  string
}
