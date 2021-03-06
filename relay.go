package main

import (
	"code.google.com/p/go.crypto/ssh"
	"net"
	"time"
)

type PeerPacket struct {
	Service  string
	Message  string
	Host     string
	Salt     string // Make sure that this is uniq, else ur packet is gonna get dropped.
	LastSeen int64
}

func HandleNorthStarChan(Chan ssh.Channel, nConn net.Conn, sshconn ssh.Conn) {
	logger.Printf("New peer %s", nConn.RemoteAddr().String())
	// First we need to add the host into the peer list.
	WriteChan := make(chan []byte)
	NewPeer := Peer{}
	NewPeer.Alive = true
	NewPeer.ApparentIP = nConn.RemoteAddr().String()
	NewPeer.Conn = sshconn
	NewPeer.MessageChan = WriteChan
	GlobalPeerList.Add(&NewPeer, -1)

	go NSConnWriteDrain(WriteChan, Chan, &NewPeer)
	go NSConnReadDrain(GlobalResvChan, Chan, &NewPeer)
}

func NSConnWriteDrain(inbound chan []byte, Chan ssh.Channel, Owner *Peer) {
	logger.Printf("[W] Peer connection has Started! for [%s]", Owner.ApparentIP)

	for outgoing := range inbound {
		_, err := Chan.Write(outgoing)
		if err != nil {
			logger.Printf("[W] Peer connection has closed for [%s] RSN: %s", Owner.ApparentIP, err.Error())
			debuglogger.Printf("Connection Write Drain shutdown.")
			Owner.m.Lock()
			if Owner.Alive {
				Owner.Alive = false
				close(Owner.MessageChan)
				Owner.MessageChan = nil
			}
			Owner.m.Unlock()

			return
		}
		debuglogger.Printf("[->] %s -> %d bytes", Owner.ApparentIP, len(outgoing))
		Owner.LastSeen = time.Now().Unix()
	}
}

func NSConnReadDrain(inbound chan []byte, Chan ssh.Channel, Owner *Peer) {
	logger.Printf("[R] Peer connection has Started! for [%s]", Owner.ApparentIP)

	buffer := make([]byte, 25565)
	var ReadLimitTime int64
	var PacketsRead int

	for {
		amt, err := Chan.Read(buffer)
		if err != nil {
			logger.Printf("[R] Peer connection has closed for [%s] RSN: %s", Owner.ApparentIP, err.Error())
			debuglogger.Printf("Connection Read Drain shutdown.")
			Owner.m.Lock()
			if Owner.Alive {
				Owner.Alive = false
				close(Owner.MessageChan)
				Owner.MessageChan = nil
			}
			Owner.m.Unlock()

			return
		}
		debuglogger.Printf("[<-] Me <- %s %d bytes", Owner.ApparentIP, amt)

		if ReadLimitTime != time.Now().Unix() {
			PacketsRead = 0
			ReadLimitTime = time.Now().Unix()
		}

		PacketsRead++
		if PacketsRead > PacketRateLimit {
			logger.Printf("Rate limit kicked in for %s This is a sign of heavy traffic of (proabbly bugs)", Owner.ApparentIP)
			if LogDroppedPackets {
				droppedlogger.Println("%x", buffer[:amt])
			}
			continue
		}
		inbound <- buffer[:amt]

	}
}
