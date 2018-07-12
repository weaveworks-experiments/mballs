// mweb is a program to demo multicast.
// Run it multiple times on different machines/containers and each
// instance will learn about the others through multicast.
// Hit it via http on port 8080 and it will return a list of instances.
// Flag --iface makes it use (and wait for) a particular interface (e.g. ethwe)
// Flag -p makes it listen on a different http port
package main

import (
	"bytes"
	"encoding/gob"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ipv4Addr = &net.UDPAddr{
		IP:   net.ParseIP("224.1.2.3"),
		Port: 7777,
	}

	peerCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "multicast",
			Subsystem: "peers",
			Name:      "total",
			Help:      "The total number of multicast peers.",
		},
	)
)

type PeerInfo struct {
	ID   int
	Name string
}

type Peer struct {
	info      PeerInfo
	addr      net.Addr
	lastHeard time.Time
}

var allPeers map[int]*Peer = make(map[int]*Peer)
var peersLock sync.Mutex

func listPeers() []string {
	peersLock.Lock()
	defer peersLock.Unlock()
	peers := []string{}
	for _, p := range allPeers {
		peers = append(peers, fmt.Sprintf("%s %s\n", p.info.Name, p.addr))
	}
	peerCount.Set(float64(len(peers)))
	return peers
}

func main() {
	var (
		ifaceName string
		httpPort  int
		err       error
	)
	flag.StringVar(&ifaceName, "iface", "eth0", "name of interface for multicasting")
	flag.IntVar(&httpPort, "p", 8080, "port to listen for http")
	flag.Parse()
	var iface *net.Interface = nil
	if ifaceName != "" {
		iface, err = ensureInterface(ifaceName, 10)
		if err != nil {
			log.Fatal(err)
		}
	}

	rand.Seed(time.Now().Unix())
	myID := rand.Int()
	conn, _ := multicastListen(iface)
	go func() {
		m := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(m)
			if err != nil {
				log.Fatal("multicast read:", err)
			}
			if n > 0 {
				decodeReceived(addr, m)
			}
		}
	}()

	sendconn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatal("send socket create:", err)
	}

	prometheus.MustRegister(peerCount)

	ticker := time.NewTicker(time.Second)
	slowerTicker := time.NewTicker(20 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				sendInfo(myID, sendconn)
				expirePeers()
			case <-slowerTicker.C:
				for _, p := range listPeers() {
					log.Printf(p)
				}
			}
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, strings.Join(listPeers(), "\n"))
	})
	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil)
	log.Fatal(err)
}

func sendInfo(id int, conn *net.UDPConn) {
	buf := new(bytes.Buffer)
	hostname, _ := os.Hostname()
	gob.NewEncoder(buf).Encode(PeerInfo{id, hostname})
	conn.WriteTo(buf.Bytes(), ipv4Addr)
}

func decodeReceived(addr net.Addr, buf []byte) {
	reader := bytes.NewReader(buf)
	decoder := gob.NewDecoder(reader)
	var info PeerInfo
	decoder.Decode(&info)
	peersLock.Lock()
	defer peersLock.Unlock()
	allPeers[info.ID] = &Peer{info, addr, time.Now()}
}

// Take out anyone we haven't heard from in a while
func expirePeers() {
	peersLock.Lock()
	defer peersLock.Unlock()
	for key, peer := range allPeers {
		if peer.lastHeard.Add(time.Second * 3).Before(time.Now()) {
			delete(allPeers, key)
		}
	}
}

func multicastListen(iface *net.Interface) (*net.UDPConn, error) {
	conn, err := net.ListenMulticastUDP("udp", iface, ipv4Addr)
	if err != nil {
		log.Fatal("multicast create:", err)
	}
	return conn, err
}

func ensureInterface(ifaceName string, wait int) (iface *net.Interface, err error) {
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
