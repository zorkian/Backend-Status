/* backend-status-server.go
 *
 * Listens on a given UDP port and accepts status packets from a Perlbal with the
 * BackendStatus plugin active.  Collates, stores, and displays information about
 * what each backend is doing.
 *
 * by Mark Smith <mark@qq.is>
 *
 * TODO: if a Perlbal goes away, we might have requests stuck in the InFlight map
 * that are no longer in flight.  we should have a timeout at which point we assume
 * something is dead and move on.
 *
 */

package main

import (
	"container/vector"
	"flag"
	"http"
	"json"
	"log"
	"net"
	"strconv"
	"time"
)

type RequestId string // string for marshaling
type BackendIpport string
type BackendMap map[BackendIpport]*Backend
type RequestMap map[RequestId]*Request
type ReceiverMap map[string]RequestMap

// this is very non-verbose because we pass this data over the wire, and field names
// are encoded in JSON and repeated a lot
type BackendUpdate struct {
	I int     // the ID for this request (must be unique for a given backend)
	B string  // the backend in "ip:port" form
	C byte    // the type of this update; 1 = request sent, 2 = request ended
	T float64 // if c == 2, the amount of time this request spent processing
	R int     // if c == 2, the status code of the response, with special codes**
	U string  // if c == 1, the uri that was sent to the backend (WITH query string)    
}

type Request struct {
	Id           RequestId // unique ID for this request on this backend (NOT globally unique)
	Uri          string    // includes query string
	Time         float64   // seconds it took to complete
	StartTime    int64     // time since we first heard about it
	ResponseCode uint16    // may be a special code**
}

type Backend struct {
	Ipport    BackendIpport // "ip:port" of this backend
	Completed vector.Vector // requests that have finished
	InFlight  ReceiverMap   // requests in flight, there could be many
}

type JsonWorld struct {
    CurrentTime int64
    World *BackendMap
}

// contains all of our backends so we can track things
var World BackendMap = make(BackendMap)

// **an interlude about special codes.  Perlbal might send in a special code if there
// was a failure or other timeout internally and the response code is not actually from
// the backend itself.
//
// TODO: <<list of codes goes here>>

func main() {
	// flag parsing
	var listen *string = flag.String("listen", "127.0.0.1:9463", "IP:port to listen for traffic on")
	var serve *string = flag.String("serve", "127.0.0.1:9464", "IP:port to serve the JSON object on")
	flag.Parse()

	// establish the listening connection
	conn, err := net.ListenPacket("udp", *listen)
	if err != nil {
		log.Fatalf("Listen failed: %s", err)
	}

	// setup a channel that reads out backend updates.  allow buffering up to 100
	// of these before we start blocking...
	go readUpdates(conn)
	log.Print("Okay, we're up and running.")

	// now listen for people to ask us for pages and serve up a JSON object
	http.Handle("/world.json", http.HandlerFunc(writeWorld))
	err = http.ListenAndServe(*serve, nil)
	if err != nil {
		log.Fatalf("ListenAndServe: %s", err)
	}
}

func readUpdates(conn net.PacketConn) {
	var buf [4096]byte

	for {
		n, addr, err := conn.ReadFrom(buf[0:])
		if err != nil {
			log.Fatalf("Error in read: %s", err)
		}

		addrstr := addr.String()

		var update BackendUpdate
		err = json.Unmarshal(buf[0:n], &update)
		if err != nil {
			log.Printf("Failed to unmarshal JSON: %s", err)
			continue
		}

		// loosely sanity check the input structure
		if len(update.B) == 0 {
			log.Print("Got invalid structure, no B value.")
			continue
		}

        // type conversions
        ipport := BackendIpport(update.B)
        reqid := RequestId(strconv.Itoa(update.I))

		// no matter what, we will be getting data on a backend, so build it if it's new
		backend, ok := World[ipport]
		if !ok {
			backend = &Backend{Ipport: ipport, InFlight: make(ReceiverMap)}
			World[ipport] = backend
		}

		// we also have to build the map for this Perlbal in the InFlight map
		_, ok = backend.InFlight[addrstr]
		if !ok {
			backend.InFlight[addrstr] = make(RequestMap)
		}

		// now do something with this update...
		switch update.C {
		case 1: // new request
			request := &Request{Id: reqid, Uri: update.U, StartTime: time.Nanoseconds()}
			_, ok := backend.InFlight[addrstr][request.Id]
			if ok {
				// if this happens, then we got two "new request" commands with the same id on the
				// same backend.  this should not happen, but if it does, we have to throw both away
				// because we can't guarantee what will happen when we get a finished response.  it
				// is better to have no data than bad data.
				log.Printf("Violated constraint, duplicate Id %s used on a backend.", request.Id)
				backend.InFlight[addrstr][request.Id] = nil, false
				continue
			}

			backend.InFlight[addrstr][request.Id] = request

			// DEBUGGING
			//log.Printf("start: addr=%s id=%s uri=%s", addrstr, request.Id, request.Uri)

		case 2: // request done
			request, ok := backend.InFlight[addrstr][reqid]
			if !ok {
				log.Printf("Request Id %s unknown.", reqid)
				continue
			}

			request.Time = update.T
			request.ResponseCode = uint16(update.R)

			backend.InFlight[addrstr][request.Id] = nil, false
			backend.Completed.Insert(0, request)
			if backend.Completed.Len() > 500 {
				backend.Completed.Pop()
			}

			// DEBUGGING
			//log.Printf("  end: addr=%s id=%s code=%d time=%f", addrstr, request.Id, request.ResponseCode, request.Time)

		default:
			log.Print("Unknown update type: C =", update.C)
		}
	}
}

func writeWorld(w http.ResponseWriter, req *http.Request) {
    world := JsonWorld{World: &World, CurrentTime: time.Nanoseconds()}
	bytes, err := json.Marshal(world)
	if err != nil {
		log.Fatalf("Error writing JSON: %s", err)
	}

	w.SetHeader("Access-Control-Allow-Origin", "*")
	w.SetHeader("Access-Control-Max-Age", "3600")
	w.Write(bytes)
}
