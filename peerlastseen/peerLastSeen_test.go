package peerlastseen

import (
    "fmt"
    "testing"
    "time"

    "github.com/libp2p/go-libp2p-core/peer"
)

var (
    callback = func(id peer.ID) {
        fmt.Println("\nDummy callback called for id\n", string(id))
    }
)

const (
    fakeID1 = peer.ID("Yo-I-got-a-fake-ID-though")
    fakeID2 = peer.ID("McLOVIN")
)

func TestCreatePLS(test *testing.T) {
    test.Run("CreatePLS-no-CB", func(test *testing.T) {
        pls, err := NewPeerLastSeen(5 * time.Second, nil)
        if err != nil || pls == nil {
            test.Errorf("NewPeerLastSeen() with no callback failed:\n%v", err)
        }
    })

    test.Run("CreatePLS-with-CB", func(test *testing.T) {
        pls, err := NewPeerLastSeen(5 * time.Second, callback)
        if err != nil || pls == nil {
            test.Errorf("NewPeerLastSeen() with callback failed:\n%v", err)
        }
    })

    test.Run("CreatePLS-NegDur", func(test *testing.T) {
        pls, err := NewPeerLastSeen(-5 * time.Second, callback)
        if err == nil || pls != nil {
            test.Errorf("NewPeerLastSeen() with negative duration succeeded, expected it to fail")
        }
    })
}

func TestUpdateLastSeen(test *testing.T) {
    pls := &PeerLastSeen {
        lastSeen:       make(map[peer.ID]time.Time),
        timeout:        5 * time.Second,
        cleanupActive:  false,
        callback:       callback,
    }

    // Tests case where background clean-up groutine isn't active
    test.Run("UpdateLastSeen-no-cleanup", func(test *testing.T) {
        err := pls.UpdateLastSeen(fakeID1)
        if err == nil {
            test.Fatalf("UpdateLastSeen() without cleanup goroutine succeeded, expected it to fail")
        }
    })

    // Activate background goroutine and re-test
    go pls.autoRemove()
    for pls.cleanupActive == false {
        time.Sleep(time.Millisecond)
    }

    test.Run("UpdateLastSeen", func(test *testing.T) {
        err := pls.UpdateLastSeen(fakeID1)
        if err != nil {
            test.Fatalf("UpdateLastSeen() failed:\n%v", err)
        }
    })
}

func TestLastSeen(test *testing.T) {
    pls := &PeerLastSeen {
        lastSeen:       make(map[peer.ID]time.Time),
        timeout:        5 * time.Second,
        cleanupActive:  false,
        callback:       callback,
    }

    now := time.Now()
    pls.lastSeen[fakeID2] = now

    test.Run("LastSeen-no-cleanup", func(test *testing.T) {
        _, err := pls.LastSeen(fakeID2)
        if err == nil {
            test.Fatalf("LastSeen() without cleanup goroutine succeeded, expected it to fail")
        }
    })

    // Activate background goroutine and re-test
    go pls.autoRemove()
    for pls.cleanupActive == false {
        time.Sleep(time.Millisecond)
    }

    test.Run("LastSeen", func(test *testing.T) {
        ts, err := pls.LastSeen(fakeID2)
        if err != nil {
            test.Errorf("LastSeen() returned an error:\n%v", err)
        }

        if ts != now {
            test.Errorf("LastSeen() returned timestmap %s, expected %s", ts, now)
        }
    })
}

func TestBackgroundCleanUp(test *testing.T) {
    pls := &PeerLastSeen {
        lastSeen:       make(map[peer.ID]time.Time),
        timeout:        5 * time.Second,
        cleanupActive:  false,
        callback:       callback,
    }

    // Activate background goroutine and re-test
    go pls.autoRemove()
    for pls.cleanupActive == false {
        time.Sleep(time.Millisecond)
    }

    // Insert 2 IDs, spaced apart by 3 seconds.
    // Then do a sanity-check to see if the length of lastSeen is 2.
    // Wait another 3 seconds (past the timeout period) and check the
    // length of lastSeen is 1.
    pls.lastSeen[fakeID1] = time.Now()
    time.Sleep(3 * time.Second)
    pls.lastSeen[fakeID2] = time.Now()

    if len(pls.lastSeen) != 2 {
        test.Fatalf("Expected length of 'lastSeen' in PLS to be 2, but was %d", len(pls.lastSeen))
    }

    time.Sleep(3 * time.Second)

    if len(pls.lastSeen) != 1 {
        test.Fatalf("Expected length of 'lastSeen' in PLS to be 1, but was %d", len(pls.lastSeen))
    }
}

