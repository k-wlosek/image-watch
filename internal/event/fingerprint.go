package event

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Fingerprint computes the notification deduplication identity for an event.
func Fingerprint(e Event) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s",
		e.Image.Registry,
		e.Image.Repository,
		e.CurrentTag,
		e.Platform.String(),
		e.Type,
		e.CandidateTag,
		e.CandidateDigest,
	)
	return hex.EncodeToString(h.Sum(nil))
}
