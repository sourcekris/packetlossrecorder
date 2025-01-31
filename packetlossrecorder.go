package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	ping "github.com/prometheus-community/pro-bing"
)

const pingHost = "google.com"

type packetLossState struct {
	timeLastSuccess    time.Time
	timeLastPacketLoss time.Time
	packetsLost        int
	packetLossState    bool
	packetLossCounter  float64
}

func main() {
	pinger, err := ping.NewPinger(pingHost)
	if err != nil {
		fmt.Printf("Ping failed with error: %v", err)
		os.Exit(-1)
	}

	// Listen for Ctrl-C.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			pinger.Stop()
		}
	}()

	// Windows requires this.
	pinger.SetPrivileged(true)

	p := new(packetLossState)

	pinger.OnRecv = func(pkt *ping.Packet) {
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v\n",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt)

		if p.packetLossState {
			p.packetLossState = false
			p.timeLastSuccess = time.Now()
			fmt.Printf("Recovered packet loss state at: %v - Lost %v packets.\n", p.timeLastSuccess, p.packetsLost)
		}
	}

	pinger.OnDuplicateRecv = func(pkt *ping.Packet) {
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v ttl=%v (DUP!)\n",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.TTL)
	}

	pinger.OnSendError = func(pkt *ping.Packet, err error) {
		fmt.Printf("error sending packet: %v", err)
	}

	pinger.OnSend = func(pkt *ping.Packet) {
		stats := pinger.Statistics()
		if stats.PacketLoss > p.packetLossCounter {
			// Update the state
			if !p.packetLossState {
				p.packetLossState = true
				p.timeLastPacketLoss = time.Now()
			}

			p.packetsLost = p.packetsLost + 1
			p.packetLossCounter = stats.PacketLoss

			fmt.Printf("Entered packet loss state at: %v - Lost %v packets so far.\n", p.timeLastPacketLoss, p.packetsLost)
		}
	}

	pinger.OnFinish = func(stats *ping.Statistics) {
		fmt.Printf("\n--- %s ping statistics ---\n", stats.Addr)
		fmt.Printf("%d packets transmitted, %d packets received, %v%% packet loss\n",
			stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss)
		fmt.Printf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n",
			stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
	}

	fmt.Printf("PING %s (%s):\n", pinger.Addr(), pinger.IPAddr())
	err = pinger.Run()
	if err != nil {
		panic(err)
	}
}
