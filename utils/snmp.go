package utils

import (
	"fmt"
	"log"
	"os"
	"time"

	g "github.com/gosnmp/gosnmp"
)

func OltSnmpConnect(host string, community string, maxOids int, maxRepetitions uint32, validatePing bool) (*g.GoSNMP, error) {
	// if true then check if ping is ok to host
	if validatePing {
		if err := PingHost(host, 3, 2); err != nil {
			return nil, err
		}
	}

	params := &g.GoSNMP{
		Community:          community,
		Target:             host,
		Port:               161,
		Version:            g.Version2c,
		Timeout:            2 * time.Second,
		MaxOids:            maxOids,        // Limit the number of OIDs per request
		MaxRepetitions:     maxRepetitions, // Limit the number of rows per OID
		Retries:            3,
		ExponentialTimeout: true,
		Logger:             g.NewLogger(log.New(os.Stdout, "", 0)),
	}
	err := params.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to snmp: %v", err)
	}

	return params, nil
}

// func SnmpConnectV2(host string, community string, validatePing bool) (*gi.SNMP, error) {
// }
