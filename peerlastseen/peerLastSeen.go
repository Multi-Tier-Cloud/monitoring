package peerlastseen

import (
    "errors"
    "sync"
    "time"

    "github.com/libp2p/go-libp2p-core/peer"
)

type PeerLastSeenCB func(peer.ID)

// TODO: Merge PeerLastSeen code into common/p2putil?
// Similar to libp2p's 'identify' package's ObservedAddr, but for IDs rather
// than specific multiaddresses
type PeerLastSeen struct {
    mutex           sync.RWMutex
    lastSeen        map[peer.ID]time.Time

    // Peers last seen longer than the timeout duration will be removed.
    timeout         time.Duration

    // Indicates if the background peer clean-up goroutine is active.
    // The info in this data structure should be considered stale
    // if the goroutine is offline.
    cleanupActive   bool

    // Optional callback invoked by background clean-up goroutine whenever
    // a peer's timeout is up. Up to user to ensure its thread-safe.
    callback        PeerLastSeenCB
}

func NewPeerLastSeen(dur time.Duration, cb PeerLastSeenCB) (*PeerLastSeen, error) {
    if (dur <= 0) {
        return nil, errors.New("Unable to set timeout to a value <= 0")
    }

    pls := &PeerLastSeen {
        lastSeen:       make(map[peer.ID]time.Time),
        timeout:        dur,
        cleanupActive:  false,
        callback:       cb,
    }

    // Start background goroutine and wait for it to come up
    go pls.autoRemove()
    for pls.cleanupActive == false {
        time.Sleep(time.Millisecond)
    }

    return pls, nil
}

func (pls *PeerLastSeen) UpdateLastSeen(id peer.ID) error {
    now := time.Now()
    if pls.cleanupActive == false {
        return errors.New("Background clean-up goroutine is offline")
    }

    pls.mutex.Lock()
    defer pls.mutex.Unlock()

    pls.lastSeen[id] = now
    return nil
}

// Returns timestamp of when peer with given 'id' was last seen
// Use absolute time rather than duration in case 'id' is not in
// the map and we return the default 0-value
func (pls *PeerLastSeen) LastSeen(id peer.ID) (time.Time, error) {
    if pls.cleanupActive == false {
        return time.Time{}, errors.New("Background clean-up goroutine is offline")
    }

    pls.mutex.RLock()
    defer pls.mutex.RUnlock()

    timestamp := pls.lastSeen[id]
    return timestamp, nil
}

// Creates a background goroutine to automatically clear peers
// that are older than the 'timeout' value set
func (pls *PeerLastSeen) autoRemove() {
    if pls.cleanupActive {
        return // This should not be called twice...
    }

    pls.cleanupActive = true
    defer func() {
        // If this goroutine ever crashes, all other methods should fail
        pls.cleanupActive = false
    }()

    // Currently will check at intervals of *minimum* 1 second
    // In reality the intervals may be larger, depending on how many peers
    // are needed to be checked (range over all of 'lastSeen')
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    doneRound := make(chan bool)
    for {
        <-ticker.C

        // Put cleanup process in a separate goroutine so the mutex defer
        // is local within it, otherwise the defer will apply to this
        // scope, and will never be Unlock()'d so long as its running.
        //
        // Could manually call Unlock(), but there's a chance of
        // a panic/crash, and Unlock() would never be called.
        //
        // Use 'doneRound' to signal when its safe to go to the next round,
        // avoiding scenario where multiple clean-up goroutines will run
        // if ranging over 'lastSeen' takes over 1 second.
        go func() {
            pls.mutex.Lock()
            defer pls.mutex.Unlock()

            for id, ts := range(pls.lastSeen) {
                if time.Since(ts) > pls.timeout {
                    delete(pls.lastSeen, id)

                    if pls.callback != nil {
                        go pls.callback(id)
                    }
                }
            }

            doneRound <- true
        }()

        <-doneRound
    }
}


