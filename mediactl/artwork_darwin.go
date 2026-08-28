//go:build darwin && cgo

package mediactl

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework AppKit
#include <stdlib.h>
#include <stdint.h>

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>

// Remote artwork cache. The run loop only reads decoded images through
// cachedRemoteArtwork; the network fetch happens in Go on a background
// goroutine, whose result lands here via setRemoteArtworkData.
static NSMutableDictionary *gRemoteArtwork;
static NSMutableArray *gRemoteArtworkOrder;

static void ensureRemoteArtworkCache(void) {
	if (gRemoteArtwork == nil) {
		gRemoteArtwork = [[NSMutableDictionary alloc] init];
		gRemoteArtworkOrder = [[NSMutableArray alloc] init];
	}
}

// Non-static: called from the run-loop code in service_darwin.go.
NSImage *cachedRemoteArtwork(NSURL *url) {
	ensureRemoteArtworkCache();
	@synchronized(gRemoteArtwork) {
		return gRemoteArtwork[url.absoluteString];
	}
}

// remoteArtworkCount returns how many decoded artworks are currently cached.
// Test-only introspection.
int remoteArtworkCount(void) {
	ensureRemoteArtworkCache();
	@synchronized(gRemoteArtwork) {
		return (int)[gRemoteArtwork count];
	}
}

// setRemoteArtworkData stores decoded artwork for a remote URL. Called from a
// background Go goroutine; never on the run loop. The bytes are copied, so the
// caller may free its buffer immediately.
static void setRemoteArtworkData(const char *url, const void *bytes, size_t len) {
	@autoreleasepool {
		ensureRemoteArtworkCache();
		NSString *key = [NSString stringWithUTF8String:url];
		NSData *data = [NSData dataWithBytes:bytes length:len];
		NSImage *image = [[[NSImage alloc] initWithData:data] autorelease];
		if (image == nil) {
			return;
		}
		@synchronized(gRemoteArtwork) {
			if (gRemoteArtwork[key] == nil) {
				gRemoteArtwork[key] = image;
				[gRemoteArtworkOrder addObject:key];
				if ([gRemoteArtworkOrder count] > 64) {
					[gRemoteArtwork removeObjectForKey:gRemoteArtworkOrder[0]];
					[gRemoteArtworkOrder removeObjectAtIndex:0];
				}
			}
		}
	}
}
*/
import "C"

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const (
	// remoteArtworkMaxBytes caps how much artwork a single fetch accepts.
	remoteArtworkMaxBytes = 10 << 20
	// remoteArtworkTimeout bounds the whole fetch, network included.
	remoteArtworkTimeout = 15 * time.Second
	// remoteArtworkRetryAfter is the cooldown before retrying a failed URL.
	remoteArtworkRetryAfter = time.Minute
)

var (
	artMu       sync.Mutex
	artBytes    = map[string][]byte{}    // successful fetches
	artFailed   = map[string]time.Time{} // failed URLs -> retry after
	artInFlight = map[string]bool{}      // fetches currently running
)

// remoteArtworkCachedCount returns how many decoded artworks are held in the
// Objective-C cache. Test-only introspection.
func remoteArtworkCachedCount() int {
	return int(C.remoteArtworkCount())
}

// isRemoteArtURL reports whether the artwork URL must be fetched over the
// network. Local file:// artwork is loaded synchronously on the run loop; only
// remote URLs go through the background fetcher.
func isRemoteArtURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// scheduleRemoteArtwork ensures a single background fetch for url. The decoded
// result is handed to the Objective-C cache, so subsequent Now Playing updates
// publish artwork without touching the network again.
func scheduleRemoteArtwork(url string) {
	artMu.Lock()
	defer artMu.Unlock()

	if _, ok := artBytes[url]; ok {
		return
	}
	if last, ok := artFailed[url]; ok && time.Since(last) < remoteArtworkRetryAfter {
		return
	}
	if artInFlight[url] {
		return
	}
	artInFlight[url] = true
	go fetchRemoteArtwork(url)
}

func fetchRemoteArtwork(url string) {
	defer func() {
		artMu.Lock()
		delete(artInFlight, url)
		artMu.Unlock()
	}()

	client := &http.Client{Timeout: remoteArtworkTimeout}
	resp, err := client.Get(url)
	if err != nil {
		artMu.Lock()
		artFailed[url] = time.Now()
		artMu.Unlock()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		artMu.Lock()
		artFailed[url] = time.Now()
		artMu.Unlock()
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, remoteArtworkMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > remoteArtworkMaxBytes {
		artMu.Lock()
		artFailed[url] = time.Now()
		artMu.Unlock()
		return
	}

	artMu.Lock()
	artBytes[url] = data
	delete(artFailed, url)
	artMu.Unlock()

	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	buf := C.CBytes(data)
	defer C.free(unsafe.Pointer(buf))
	C.setRemoteArtworkData(cURL, buf, C.size_t(len(data)))
}
