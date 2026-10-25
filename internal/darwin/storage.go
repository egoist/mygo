//go:build darwin

package darwin

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// A page with a StorageID (platform.WindowOptions) keeps its website data
// in the WKWebsiteDataStore of an identifier (macOS 14), which WebKit
// writes under ~/Library/WebKit/<bundle id>/WebsiteDataStore/<identifier>
// and finds again at the next launch. The identifier is an NSUUID, so the
// StorageID becomes one, the same each time: a version 5 UUID of it, in a
// namespace of MyGo's own.

// storageNamespace is the namespace of the UUIDs of StorageIDs,
// 9f1c5a7e-3b0d-4c52-8a61-6d2e40b7f3a9. Changing it would lose the data
// stores of every app.
var storageNamespace = [16]byte{0x9f, 0x1c, 0x5a, 0x7e, 0x3b, 0x0d, 0x4c, 0x52, 0x8a, 0x61, 0x6d, 0x2e, 0x40, 0xb7, 0xf3, 0xa9}

// storageUUID returns the UUID, in its text form, of the data store of a
// StorageID.
func storageUUID(storageID string) string {
	h := sha1.New()
	h.Write(storageNamespace[:])
	h.Write([]byte(storageID))
	u := h.Sum(nil)[:16]
	u[6] = u[6]&0x0f | 0x50 // version 5
	u[8] = u[8]&0x3f | 0x80 // the variant of RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// checkStorage fails with ErrUnsupported when this macOS cannot keep the
// data of a StorageID apart: data stores of an identifier came with macOS
// 14.
func checkStorage(storageID string) error {
	if storageID == "" || respondsTo(class("WKWebsiteDataStore"), "dataStoreForIdentifier:") {
		return nil
	}
	return fmt.Errorf("mygo: PageOptions.StorageID needs macOS 14 or newer: %w", platform.ErrUnsupported)
}

// dataStore returns the data store of a StorageID, autoreleased.
// checkStorage said it exists.
func dataStore(storageID string) id {
	uuid := send(send(class("NSUUID"), "alloc"), "initWithUUIDString:", uintptr(nsString(storageUUID(storageID))))
	defer release(uuid)
	return send(class("WKWebsiteDataStore"), "dataStoreForIdentifier:", uintptr(uuid))
}

// ClearStorage deletes all the website data of a StorageID's data store,
// as ClearBrowsingData does of the default one. It clears the store
// rather than removing it (removeDataStoreForIdentifier:), which fails
// while a web view uses the store. A store that does not exist has no
// data, and asking for it with dataStoreForIdentifier: would make it, so
// the call looks for it among the stores WebKit lists first.
func (a appController) ClearStorage(storageID string, done func(error)) {
	if err := checkStorage(storageID); err != nil {
		done(err)
		return
	}
	withPool(func() {
		// Listing stores before WebKit has started crashes it
		// (fetchAllDataStoreIdentifiers: dispatches to a run loop that does
		// not exist yet); making a configuration starts it.
		release(send(send(class("WKWebViewConfiguration"), "alloc"), "init"))
		listed := newBlock(func(_ objc.Block, identifiers id) {
			// The array is the block's own, and WebKit may call from any
			// thread: the lookup is made here, the clearing on main.
			found := storeListed(identifiers, storageUUID(storageID))
			a.b.runOnMain(func() {
				if !found {
					done(nil)
					return
				}
				withPool(func() { a.clearDataStore(dataStore(storageID), done) })
			})
		})
		send(class("WKWebsiteDataStore"), "fetchAllDataStoreIdentifiers:", uintptr(listed))
		listed.Release()
	})
}

// storeListed reports whether the NSArray of NSUUIDs identifiers has the
// UUID uuid, in its text form.
func storeListed(identifiers id, uuid string) (found bool) {
	withPool(func() {
		for _, u := range arrayItems(identifiers) {
			if strings.EqualFold(goString(send(u, "UUIDString")), uuid) {
				found = true
			}
		}
	})
	return found
}
