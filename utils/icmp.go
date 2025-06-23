package utils

import (
	"fmt"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

func PingHost(host string, count int, minPktsRecieved int) error {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		Logline("Error creating pinger", host, err)
		return err
	}

	// Set ping parameters
	pinger.Count = count
	pinger.Timeout = 3 * time.Second

	// Run the ping
	err = pinger.Run()
	if err != nil {
		Logline("Error running ping on host", host, err)
		return err
	}

	stats := pinger.Statistics()
	if stats.PacketsRecv < minPktsRecieved {
		return fmt.Errorf("host %s seems to be down or not responding. %d packets received out of %d sent", host, stats.PacketsRecv, stats.PacketsSent)
	}
	return nil
}
