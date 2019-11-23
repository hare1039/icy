package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/pion/webrtc"
)

func main() {
	addr := flag.String("address", ":51632", "HTTP server for exange candidates")
	offer := flag.Bool("offer", false, "exposed service")
	exposeAddr := flag.String("expose", "localhost:22", "exposed service")
	listenerAddr := flag.String("listen", ":10000", "local listener for remote service(e.g. ssh)")
	help := flag.Bool("help", false, "help")
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *offer {
		fmt.Println("offer (client) mode")
	} else {
		fmt.Println("answer (server) mode")
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:140.113.56.70:3478", "stun:hare1039.nctu.me:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}

	if !*offer {
		// serve peer candidate
		peer := exposeServer(config, *exposeAddr)
		offerChan, answerChan := httpSignal(*addr)
		offer := <-offerChan

		if err := peer.SetRemoteDescription(offer); err != nil {
			panic(err)
		}

		// Create answer
		answer, err := peer.CreateAnswer(nil)
		if err != nil {
			panic(err)
		}
		if err = peer.SetLocalDescription(answer); err != nil {
			panic(err)
		}

		// Send the answer
		answerChan <- answer
	} else {
		peer := offerClient(config, *listenerAddr)
		// Create an offer to send to the browser
		offer, err := peer.CreateOffer(nil)
		if err != nil {
			panic(err)
		}

		// Sets the LocalDescription, and starts our UDP listeners
		err = peer.SetLocalDescription(offer)
		if err != nil {
			panic(err)
		}

		// Exchange the offer for the answer
		answer := offerSignal(offer, *addr)

		// Apply the answer as the remote description
		err = peer.SetRemoteDescription(answer)
		if err != nil {
			panic(err)
		}
	}
	// Block forever
	select {}
}

func exposeServer(config webrtc.Configuration, exposeAddr string) *webrtc.PeerConnection {
	conn, err := net.Dial("tcp", exposeAddr)

	peer, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	peer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State changed: %s\n", state.String())
	})

	peer.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

		d.OnOpen(func() {
			fmt.Printf("Data channel '%s'-'%d' open", d.Label(), d.ID())

			buf := make([]byte, 8192)
			reader := bufio.NewReader(conn)
			for {
				n, err := reader.Read(buf)
				if err != nil {
					panic(err)
				}

				slice := buf[0:n]
				d.Send(slice)
			}
		})

		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			conn.Write(msg.Data)
			//			fmt.Printf("Message write '%s': '%s'\n", d.Label(), String(msg.Data))
		})
	})

	return peer
}

func httpSignal(address string) (offerOut chan webrtc.SessionDescription, answerIn chan webrtc.SessionDescription) {
	offerOut = make(chan webrtc.SessionDescription)
	answerIn = make(chan webrtc.SessionDescription)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var offer webrtc.SessionDescription
		err := json.NewDecoder(r.Body).Decode(&offer)
		if err != nil {
			panic(err)
		}
		offerOut <- offer
		answer := <-answerIn
		err = json.NewEncoder(w).Encode(answer)
		if err != nil {
			panic(err)
		}
	})

	go func() {
		panic(http.ListenAndServe(address, nil))
	}()

	fmt.Println("Listening on", address)
	return
}

func offerClient(config webrtc.Configuration, listenerAddr string) *webrtc.PeerConnection {
	fmt.Println("Listen on", listenerAddr)
	ln, err := net.Listen("tcp", listenerAddr)
	if err != nil {
		panic(err)
	}

	conn, err := ln.Accept()
	if err != nil {
		panic(err)
	}

	bufio.NewReader(conn)

	peer, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	// label "data"
	d, err := peer.CreateDataChannel("data", nil)
	if err != nil {
		panic(err)
	}

	peer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State changed: %s\n", state.String())
	})

	d.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open.", d.Label(), d.ID())
		buf := make([]byte, 8192)
		reader := bufio.NewReader(conn)
		for {
			n, err := reader.Read(buf)
			if err != nil {
				panic(err)
			}

			slice := buf[0:n]
			d.Send(slice)
		}
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		conn.Write(msg.Data)
		//		fmt.Printf("Message write '%s': '%s'\n", d.Label(), String(msg.Data))
	})

	return peer
}

func offerSignal(offer webrtc.SessionDescription, address string) webrtc.SessionDescription {
	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(offer)
	if err != nil {
		panic(err)
	}

	resp, err := http.Post("http://"+address, "application/json; charset=utf-8", b)
	if err != nil {
		panic(err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			panic(closeErr)
		}
	}()

	var answer webrtc.SessionDescription
	err = json.NewDecoder(resp.Body).Decode(&answer)
	if err != nil {
		panic(err)
	}

	return answer
}
