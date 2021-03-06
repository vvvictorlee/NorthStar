package main

import (
	"code.google.com/p/go.crypto/ssh"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"
)

type Peer struct {
	HostName    string
	ApparentIP  string
	Conn        ssh.Conn
	ID          int
	Alive       bool
	LastSeen    int64
	m           sync.Mutex
	LastAttempt int64
	NodeID      string
	MessageChan chan []byte
}

type PList struct {
	Peers     map[int]*Peer
	PeerCount int
	m         sync.Mutex
}

func (p *PList) Add(n *Peer, idoverride int) {
	p.m.Lock()

	n.ApparentIP = CorrectHost(n.ApparentIP)
	if idoverride != -1 && p.Peers[idoverride].Alive == false {
		n.ID = idoverride
		p.Peers[idoverride] = n
	} else {
		p.PeerCount++
		n.ID = p.PeerCount
		p.Peers[n.ID] = n
	}

	p.m.Unlock()
}

func CorrectHost(host string) string {
	bits := strings.Split(host, ":")
	return bits[0] + ":48563"
}

func (p PList) ContainsIP(host string) bool {

	for _, v := range p.Peers {
		if v.ApparentIP == host {
			debuglogger.Printf("DEBUG %s LOOKS ALOT LIKE %s", v.ApparentIP, host)
			return true
		}
	}
	return false
}

func (p PList) FindByIP(host string) int {
	p.m.Lock()
	debuglogger.Println("GPList is locked")

	defer p.m.Unlock()
	defer debuglogger.Println("GPList is unlocked")
	for k, v := range p.Peers {
		if v.ApparentIP == host && v.Alive == false {
			return k
		}
	}
	return -1
}

func (p PList) RemoveByStruct(n Peer) {
	p.m.Lock()
	//p.Peers[n.ID].Alive = false
	if p.Peers[n.ID].Conn != nil {
		p.Peers[n.ID].Conn.Close()
	}
	p.m.Unlock()
}

var GlobalPeerList PList

func StartLookingForPeers() {
	GlobalPeerList = PList{}
	GlobalPeerList.Peers = make(map[int]*Peer)
	GlobalPeerList.PeerCount = 0
	RestorePeerList()

	hash := HashValue([]byte(CC_KEY))
	inboundchan := StartDHT(fmt.Sprintf("%x", hash[:20]))
	for host := range inboundchan {
		if !GlobalPeerList.ContainsIP(host) {
			NewPeer := Peer{}
			NewPeer.Alive = false
			NewPeer.ApparentIP = host
			GlobalPeerList.Add(&NewPeer, -1)
			debuglogger.Printf("DEBUG: Added new peer to the peer list, Host is %s", host)
		}
	}
}

func SystemCleanup() {
	for {
		var SaveList string
		time.Sleep(time.Minute)
		GlobalPeerList.m.Lock()
		debuglogger.Println("GPList is locked")

		for _, v := range GlobalPeerList.Peers {
			if v.Alive {
				SaveList = SaveList + strings.Split(v.ApparentIP, ":")[0] + ":48563\n"
			}
		}
		GlobalPeerList.m.Unlock()
		debuglogger.Println("GPList is unlocked")
		err := ioutil.WriteFile("/.nspeerlistcache", []byte(SaveList), 660)

		if err != nil {
			debuglogger.Printf("Unable to save peer list to a cache because of %s", err)
		} else {
			debuglogger.Printf("Saved Peer list")
		}

		// Now scan the peer list for connections that are open but have not had convo for +120 seconds
		GlobalPeerList.m.Lock()
		debuglogger.Println("GPList is locked")

		for _, v := range GlobalPeerList.Peers {
			if v.Alive && v.LastSeen < time.Now().Unix()-120 {
				v.m.Lock()
				v.Alive = false
				v.Conn.Close()
				v.m.Unlock()
				logger.Printf("[!] Purged dead looking connection from host %s", v.ApparentIP)
			} else if v.LastSeen == 0 {
				v.LastSeen = time.Now().Unix()
			}
		}

		bucket := make(map[string]int)
		for k, v := range GlobalPeerList.Peers {
			if !v.Alive && bucket[v.ApparentIP] > 2 {
				delete(GlobalPeerList.Peers, k)
				logger.Printf("[!] Garbage Collected %s from the peer list to avoid dupes", v.ApparentIP)
			} else if !v.Alive {
				bucket[v.ApparentIP]++
			}
		}

		bucket = make(map[string]int)
		for k, v := range GlobalPeerList.Peers {
			if v.Alive && bucket[v.ApparentIP] > 2 {
				v.Conn.Close()
				delete(GlobalPeerList.Peers, k)
				logger.Printf("[!] Garbage Collected (alive) %s from the peer list to avoid loops", v.ApparentIP)
			} else if !v.Alive {
				bucket[v.ApparentIP]++
			}
		}
		GlobalPeerList.m.Unlock()
		debuglogger.Println("GPList is unlocked")
	}
}

func RestorePeerList() {
	b, err := ioutil.ReadFile("/.nspeerlistcache")
	if err != nil {
		logger.Printf("Cannot read peer list cache, not restoring from peer list")
		return
	}

	lines := strings.Split(string(b), "\n")
	for i := 0; i < len(lines); i++ {
		if !GlobalPeerList.ContainsIP(lines[i]) && lines[i] != "" && lines[i] != "\r" {
			NewPeer := Peer{}
			NewPeer.Alive = false
			NewPeer.ApparentIP = lines[i]
			GlobalPeerList.Add(&NewPeer, -1)
		}
	}
}

func ScountOutNewPeers() {

	for {
		for k, v := range GlobalPeerList.Peers {
			if !v.Alive && v.LastAttempt+300 < time.Now().Unix() {
				debuglogger.Printf("DEBUG: Looking in the Peer list, Going to try and *connect* to from %s %d", v.ApparentIP, k)
				ConnectToPeer(v)
				v.LastAttempt = time.Now().Unix()
			}
		}
		time.Sleep(time.Second * 5)
	}
}
