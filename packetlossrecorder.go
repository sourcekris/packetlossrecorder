package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	ping "github.com/prometheus-community/pro-bing"
)

const pingHost = "google.com"

type Pinger struct {
	p               *ping.Pinger
	a               *tview.Application
	packetLossState bool
	packetsLost     int
	timeLastSuccess time.Time
	statsBox        *tview.TextView
	logBox          *tview.TextView
	mutex           sync.Mutex
	packetLossBox   *tview.TextView
	lastLossTime    time.Time
}

func NewPinger(target string, statsBox, logBox, packetLossBox *tview.TextView) (*Pinger, error) {
	pinger, err := ping.NewPinger(target)
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger: %w", err)
	}

	p := &Pinger{
		p:               pinger,
		packetLossState: false,
		packetsLost:     0,
		timeLastSuccess: time.Now(),
		statsBox:        statsBox,
		logBox:          logBox,
		packetLossBox:   packetLossBox,
	}

	// Windows requires this.
	pinger.SetPrivileged(true)

	pinger.OnRecv = p.handleRecv
	pinger.OnDuplicateRecv = func(pkt *ping.Packet) {
		p.LogMessage(fmt.Sprintf("Duplicate packet received: %v\n", pkt))
	}

	pinger.OnSend = func(pkt *ping.Packet) {
		if p.packetLossState {
			p.LogMessage(fmt.Sprintf("[red]Packet sent:[white] %v\n", pkt))
		}
	}

	return p, nil
}

func (p *Pinger) handleRecv(pkt *ping.Packet) {
	rttStr := fmt.Sprintf("%v", pkt.Rtt) // Store RTT as string for logging
	p.LogMessage(fmt.Sprintf("%d bytes from %s: icmp_seq=%d time=%s ttl=%d\n",
		pkt.Nbytes, pkt.IPAddr, pkt.Seq, rttStr, pkt.TTL))

	if p.packetLossState {
		currentLossText := p.packetLossBox.GetText(false) // Get current text
		recoverMsg := fmt.Sprintf("%s%s: [green]Ended up losing %v packets.[white]\n", currentLossText, time.Now().Format(time.RFC3339), p.packetsLost)
		p.packetLossBox.SetText(recoverMsg)
		p.packetLossBox.ScrollToEnd()

		p.packetLossState = false
		p.timeLastSuccess = time.Now()
		p.packetsLost = 0
	} else {
		p.timeLastSuccess = time.Now()
	}
}

func (p *Pinger) Run() error {
	err := p.p.Run()
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

func (p *Pinger) CheckPacketLoss(app *tview.Application) {
	time.Sleep(time.Second * 2)

	if time.Since(p.timeLastSuccess) > time.Second*3 {
		if !p.packetLossState {
			p.packetLossState = true
			p.lastLossTime = time.Now() // Store the loss time

			// Append to packetLossBox instead of overwriting
			currentLossText := p.packetLossBox.GetText(false) // Get current text
			newLossText := fmt.Sprintf("%s%s: [red]Packet Loss Detected![white]\n", currentLossText, time.Now().Format(time.RFC3339))
			p.packetLossBox.SetText(newLossText)
			p.packetLossBox.ScrollToEnd() // Scroll to bottom
		}
		p.packetsLost++
	} else if p.packetLossState { // Check for recovery
		p.packetLossState = false
		p.packetsLost = 0
	}
	app.Draw() // Redraw the UI
}

func (p *Pinger) UpdateStatsDisplay(stats *ping.Statistics) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	statsText := fmt.Sprintf("%s\nTransmitted: %d\nReceived: %d\nPacket Loss: %v%%\nMin RTT: %v\nAvg RTT: %v\nMax RTT: %v\n",
		p.p.IPAddr(), stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss, stats.MinRtt, stats.AvgRtt, stats.MaxRtt)
	p.statsBox.SetText(statsText)
}

func (p *Pinger) LogMessage(message string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	currentTime := time.Now().Format(time.RFC3339)
	logText := p.logBox.GetText(false)
	newLogText := fmt.Sprintf("%s%s: %s", logText, currentTime, message)
	p.logBox.SetText(newLogText)

	// Scroll to the bottom after setting text
	p.logBox.ScrollToEnd()
	p.a.Draw()
}

func main() {
	app := tview.NewApplication()
	statsBox := tview.NewTextView()
	statsBox.SetDynamicColors(true)
	statsBox.SetTextColor(tcell.ColorWhite)
	statsBox.SetBorder(true).SetTitle("Ping Statistics")

	packetLossBox := tview.NewTextView()
	packetLossBox.SetBorder(true)
	packetLossBox.SetTitle("Packet Loss Details")
	packetLossBox.SetDynamicColors(true)
	packetLossBox.SetTextColor(tcell.ColorWhite)
	packetLossBox.SetScrollable(true)

	logBox := tview.NewTextView()
	logBox.SetDynamicColors(true)
	logBox.SetTextColor(tcell.ColorWhite)
	logBox.SetScrollable(true) // Make the log box scrollable
	logBox.SetBorder(true).SetTitle("Ping Log")

	pinger, err := NewPinger(pingHost, statsBox, logBox, packetLossBox)
	if err != nil {
		fmt.Println("Error creating pinger:", err)
		os.Exit(1)
	}

	pinger.a = app

	// Listen for Ctrl-C.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			pinger.p.Stop()
			app.Stop()
		}
	}()

	// Flexbox for top half (statsBox and packetLossBox)
	topFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn). // Horizontal layout
		AddItem(statsBox, 0, 1, false).
		AddItem(packetLossBox, 0, 1, false)

	// Main Flexbox (topFlex and logBox)
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).   // Vertical layout
		AddItem(topFlex, 0, 1, false). // Top half
		AddItem(logBox, 0, 1, false)   // Bottom half

	go func() {
		for range time.Tick(time.Second) {
			stats := pinger.p.Statistics()
			pinger.UpdateStatsDisplay(stats)
			app.Draw()
		}
	}()

	go func() {
		for {
			pinger.CheckPacketLoss(app)
		}
	}()

	go func() { // Run the pinger in a separate goroutine
		err := pinger.Run()
		if err != nil {
			pinger.LogMessage(fmt.Sprintf("[red]Ping error: %v[white]\n", err))
			app.Draw() // Update the UI to show the error
		}
	}()

	if err := app.SetRoot(mainFlex, true).Run(); err != nil {
		panic(err)
	}

	<-c
	fmt.Println("Exiting...")
}
