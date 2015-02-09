/*
Networked behaviour:
Each node that doesn't have a ball regularly multicasts its ID, saying "I want a ball"
A node with a ball decides to send it to one other node.
Picks from the set it has heard from recently.
Node with ball multicasts "I am sending to X"
*/

package main

import (
	"bytes"
	gc "code.google.com/p/goncurses"
	"encoding/gob"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"sort"
	"time"
)

var (
	ipv4Addr = &net.UDPAddr{
		IP:   net.ParseIP("224.1.2.3"),
		Port: 7777,
	}
)

const (
	msgWantBall = iota
	msgSendBall
	msgTakeBall
)

func listen(iface *net.Interface) (*net.UDPConn, error) {
	conn, err := net.ListenMulticastUDP("udp", iface, ipv4Addr)
	if err != nil {
		log.Fatal("multicast create:", err)
	}
	return conn, err
}

const ball_height = 4
const ball_width = 12
const gravity = 1200 // delta-v per second
const updates_per_sec = 20

var ball_ascii = []string{
	` .-----.`,
	` |:: /\__/\`,
	`~|__( ^ .^ )`,
	`  ""   ""`,
}

type Object interface {
	Cleanup()
	Draw(*gc.Window)
	Update(x int, y int, offedge func(obj Object))
	SetX(x int)
	SpeedX() int
	Height() int
	NewWindow()
}

type Ball struct {
	w      *gc.Window
	Y, X   int
	Sy, Sx int
	C      int
}

func newBallWindow(y, x int, c int) *gc.Window {
	w, err := gc.NewWindow(ball_height, ball_width, y, x)
	if err != nil {
		log.Fatal("newBall:", err)
	}
	w.ColorOn(int16(c))
	for i := 0; i < len(ball_ascii); i++ {
		w.MovePrint(i, 0, ball_ascii[i])
	}
	return w
}

func newBall(y, x int, sx int) *Ball {
	c := rand.Intn(3) + 1
	w := newBallWindow(y, x, c)
	return &Ball{w, y * 100, x * 100, 0, sx, c}
}

func (b *Ball) NewWindow() {
	b.w = newBallWindow(b.Y, b.X, b.C)
}

func (s *Ball) SetX(x int)        { s.X = x }
func (s *Ball) KickX(dx int)      { s.Sx = dx }
func (s *Ball) SpeedX() int       { return s.Sx }
func (s *Ball) Height() int       { return s.Y }
func (s *Ball) Cleanup()          { s.w.Delete() }
func (s *Ball) Draw(w *gc.Window) { w.Overlay(s.w) }

func (s *Ball) Update(my, mx int, offedge func(obj Object)) {
	// Speed is positive when moving up the screen
	s.Y += s.Sy / updates_per_sec
	s.X += int(s.Sx) * 100 / updates_per_sec
	// Bounce off either side
	if s.Y < 0 {
		s.Y = -s.Y
		s.Sy = -s.Sy
	} else if s.Y > my*100 {
		s.Y = my * 100
		s.Sy = -s.Sy
	}
	if s.X < 0 {
		offedge(s)
		s.X = 0
		s.Sx = -s.Sx
	} else if (s.X + ball_width*100) > mx*100 {
		offedge(s)
		s.X = (mx - ball_width) * 100
		s.Sx = -s.Sx
	}
	s.Sy -= int(gravity / updates_per_sec)
	s.w.MoveWindow(my-ball_height-s.Y/100, s.X/100)
}

type PeerInfo struct {
	ID   byte
	Name string
}

type Peer struct {
	info      PeerInfo
	addr      net.Addr
	lastHeard time.Time
}

var allPeers map[byte]*Peer = make(map[byte]*Peer)

// PeerSlice attaches the methods of Interface to []*Peer, sorting in increasing order of id.
type PeerSlice []*Peer

func (p PeerSlice) Len() int           { return len(p) }
func (p PeerSlice) Less(i, j int) bool { return p[i].info.ID < p[j].info.ID }
func (p PeerSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

//---
type Peers struct {
	w *gc.Window
}

func newPeers(y, x int) *Peers {
	w, err := gc.NewWindow(6, 20, y, x)
	if err != nil {
		log.Fatal("newPeers:", err)
	}
	return &Peers{w}
}

func (p *Peers) NewWindow() {
	// not implemented
}

func (p *Peers) SetX(x int)  {}
func (p *Peers) SpeedX() int { return 0 }
func (p *Peers) Height() int { return ball_height }
func (p *Peers) Cleanup()    { p.w.Delete() }

func (p *Peers) Draw(w *gc.Window) {
	w.MovePrintln(0, 0, "Members: ", len(allPeers))
	var peers PeerSlice
	for _, peer := range allPeers {
		peers = append(peers, peer)
	}
	sort.Sort(peers)
	for i, peer := range peers {
		w.MovePrintln(i+1, 0, peer.addr, peer.info.Name)
	}
	w.Overlay(p.w)
}

func (p *Peers) Update(my, mx int, offedge func(obj Object)) {
	// no-op
}

//---

var objects = make([]Object, 0, 16)

func updateObjects(my, mx int, offedge func(obj Object)) {
	for _, ob := range objects {
		ob.Update(my, mx, offedge)
	}
}

func drawObjects(s *gc.Window) {
	for _, ob := range objects {
		ob.Draw(s)
	}
}

func EnsureInterface(ifaceName string, wait int) (iface *net.Interface, err error) {
	if iface, err = findInterface(ifaceName); err == nil || wait == 0 {
		return
	}
	for ; err != nil && wait > 0; wait -= 1 {
		time.Sleep(1 * time.Second)
		iface, err = findInterface(ifaceName)
	}
	return
}

func findInterface(ifaceName string) (iface *net.Interface, err error) {
	if iface, err = net.InterfaceByName(ifaceName); err != nil {
		return iface, fmt.Errorf("Unable to find interface %s", ifaceName)
	}
	if 0 == (net.FlagUp & iface.Flags) {
		return iface, fmt.Errorf("Interface %s is not up", ifaceName)
	}
	return
}

func main() {
	f, err := os.Create("err.log")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	log.SetOutput(f)

	var (
		ifaceName string
	)
	flag.StringVar(&ifaceName, "iface", "", "name of interface for multicasting")
	flag.Parse()
	var iface *net.Interface = nil
	if ifaceName != "" {
		iface, err = EnsureInterface(ifaceName, 5)
		if err != nil {
			log.Fatal(err)
		}
	}

	var stdscr *gc.Window
	stdscr, err = gc.Init()
	if err != nil {
		log.Println("Init:", err)
	}
	defer gc.End()

	gc.StartColor()
	gc.Cursor(0)
	gc.Echo(false)

	gc.InitPair(1, gc.C_WHITE, gc.C_BLACK)
	gc.InitPair(2, gc.C_YELLOW, gc.C_BLACK)
	gc.InitPair(3, gc.C_RED, gc.C_BLACK)

	lines, cols := stdscr.MaxYX()

	slowTicker := time.NewTicker(time.Second)
	frameTicker := time.NewTicker(time.Second / updates_per_sec)

	input := make(chan gc.Char)
	go func() {
		for {
			input <- gc.Char(stdscr.GetChar())
		}
	}()

	rand.Seed(time.Now().Unix())
	myID := byte(rand.Intn(256))

	conn, _ := listen(iface)
	ball_incoming := make(chan Object)
	ball_wanted := make(chan *Peer)
	go func() {
		const UDPbufSize = 1024
		m := make([]byte, UDPbufSize)
		for {
			n, addr, err := conn.ReadFrom(m)
			if err != nil {
				log.Fatal("multicast read:", err)
			}
			if n > 0 {
				reader := bytes.NewReader(m[1:])
				decoder := gob.NewDecoder(reader)
				switch m[0] {
				case msgWantBall:
					var info PeerInfo
					decoder.Decode(&info)
					if info.ID != myID {
						ball_wanted <- &Peer{info, addr, time.Now()}
					}
				case msgSendBall:
					var ball Ball
					var id byte
					decoder.Decode(&id)
					if id == myID {
						decoder.Decode(&ball)
						ball_incoming <- &ball
					}
				case msgTakeBall:
				}
			}
		}
	}()

	sendconn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatal("send socket create:", err)
	}

	pp := newPeers(0, 0)
	objects = append(objects, pp)

	sendWant := func() {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		hostname, _ := os.Hostname()
		enc.Encode(PeerInfo{myID, hostname})
		sendconn.WriteTo(append([]byte{msgWantBall}, buf.Bytes()...), ipv4Addr)
	}
	sendBallTo := func(ball Object, dest byte) {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		enc.Encode(dest)
		enc.Encode(ball)
		sendconn.WriteTo(append([]byte{msgSendBall}, buf.Bytes()...), ipv4Addr)
		for i, o := range objects {
			if o == ball {
				objects = append(objects[:i], objects[i+1:]...)
				break
			}
		}
		ball.Cleanup()
	}

	addBall := func() {
		ball := newBall(lines/2, cols/2, 0)
		objects = append(objects, ball)
	}

	receiveBall := func(ball Object) {
		x := 0
		if ball.SpeedX() < 0 {
			x = (cols - ball_width) * 100
		}
		ball.SetX(x)
		ball.NewWindow()
		objects = append(objects, ball)
	}

loop:
	for {
		stdscr.Erase()
		drawObjects(stdscr)
		stdscr.Refresh()
		select {
		case <-slowTicker.C:
			sendWant()
			for key, peer := range allPeers {
				if peer.lastHeard.Add(time.Second).Before(time.Now()) {
					delete(allPeers, key)
				}
				if len(allPeers) == 0 {
					for _, obj := range objects {
						if ball, ok := obj.(*Ball); ok {
							ball.KickX(0)
						}
					}
				}
			}
		case <-frameTicker.C:
			y, x := stdscr.MaxYX()
			updateObjects(y, x, func(obj Object) {
				if len(allPeers) != 0 {
					// pick a peer at random
					c := rand.Intn(len(allPeers))
					for _, p := range allPeers {
						if c == 0 {
							sendBallTo(obj, p.info.ID)
							break
						}
						c--
					}
				}
			})
			drawObjects(stdscr)
		case peer := <-ball_wanted:
			allPeers[peer.info.ID] = peer
			for _, obj := range objects {
				if ball, ok := obj.(*Ball); ok {
					if ball.SpeedX() == 0 {
						speed := 10
						if peer.info.ID < myID {
							speed = -10
						}
						ball.KickX(speed)
						break
					}
				}
			}
		case ball := <-ball_incoming:
			receiveBall(ball)
		case ch := <-input:
			switch ch {
			case 'b':
				addBall()
			case 'r':
				stdscr.Refresh()
			case 'q':
				break loop
			}
		}
	}
}
